package agent

import (
	"context"
	"encoding/json"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// gateTool is a minimal Tool implementation for parallel-dispatch tests.
// Bypasses typed[In]/schema plumbing so tests can drive behavior by-ID.
type gateTool struct {
	name         string
	parallelSafe bool
	run          func(ctx context.Context, raw json.RawMessage) (tools.Result, error)
}

func (g *gateTool) Name() string        { return g.name }
func (g *gateTool) Description() string { return "" }
func (g *gateTool) Schema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false}
}
func (g *gateTool) ParallelSafe() bool { return g.parallelSafe }
func (g *gateTool) Run(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	return g.run(ctx, raw)
}

type scriptCall struct{ ID, Name, Input string }

// parallelToolScript builds a two-script fake provider program: one assistant
// turn that emits every scriptCall as a tool_use block, then a final
// end-of-turn script.
func parallelToolScript(calls []scriptCall) []fake.Script {
	first := fake.Script{{Type: llm.EventMessageStart}}
	for _, c := range calls {
		first = append(first,
			llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: c.ID, ToolName: c.Name},
			llm.StreamEvent{Type: llm.EventToolUseDelta, ToolUseID: c.ID, PartialJSON: c.Input},
			llm.StreamEvent{Type: llm.EventToolUseStop, ToolUseID: c.ID},
		)
	}
	first = append(first, llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "tool_use"})
	final := fake.Script{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "done"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	}
	return []fake.Script{first, final}
}

func parseID(raw json.RawMessage) string {
	var in struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &in)
	return in.ID
}

func TestParallelGroupRunsConcurrently(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			id := parseID(raw)
			started <- id
			<-release
			return tools.Result{Output: id}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	calls := []scriptCall{
		{"t1", "Read", `{"id":"t1"}`},
		{"t2", "Read", `{"id":"t2"}`},
		{"t3", "Read", `{"id":"t3"}`},
		{"t4", "Read", `{"id":"t4"}`},
	}
	prov := fake.New(parallelToolScript(calls)...)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	eventsCh := agent.Submit(context.Background(), "go")

	seen := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for i := 0; i < 4; i++ {
		select {
		case id := <-started:
			seen[id] = true
		case <-timeout:
			t.Fatalf("only %d of 4 goroutines started before timeout", i)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected 4 unique starts, got %d: %v", len(seen), seen)
	}

	close(release)
	for range eventsCh {
	}
}

func TestParallelGroupPreservesResultOrder(t *testing.T) {
	var rngMu sync.Mutex
	rng := rand.New(rand.NewSource(1))

	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			id := parseID(raw)
			rngMu.Lock()
			d := time.Duration(rng.Intn(5)) * time.Millisecond
			rngMu.Unlock()
			time.Sleep(d)
			return tools.Result{Output: id}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	calls := []scriptCall{
		{"t1", "Read", `{"id":"t1"}`},
		{"t2", "Read", `{"id":"t2"}`},
		{"t3", "Read", `{"id":"t3"}`},
		{"t4", "Read", `{"id":"t4"}`},
		{"t5", "Read", `{"id":"t5"}`},
		{"t6", "Read", `{"id":"t6"}`},
	}
	prov := fake.New(parallelToolScript(calls)...)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	for range agent.Submit(context.Background(), "go") {
	}

	// History: [user, asst(tool_use), user(tool_results), asst(text)]
	var toolResultMsg llm.Message
	for _, m := range agent.history {
		if m.Role == llm.RoleUser && len(m.Content) > 0 && m.Content[0].Type == llm.ContentToolResult {
			toolResultMsg = m
		}
	}
	if len(toolResultMsg.Content) != len(calls) {
		t.Fatalf("expected %d tool result blocks, got %d", len(calls), len(toolResultMsg.Content))
	}
	for idx, c := range calls {
		got := toolResultMsg.Content[idx]
		if got.ToolUseID != c.ID {
			t.Errorf("slot %d: ToolUseID=%q want %q", idx, got.ToolUseID, c.ID)
		}
		if got.Output != c.ID {
			t.Errorf("slot %d: Output=%q want %q", idx, got.Output, c.ID)
		}
	}
}

func TestSemaphoreCapsConcurrency(t *testing.T) {
	var inflight atomic.Int32
	var maxSeen atomic.Int32

	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			n := inflight.Add(1)
			for {
				m := maxSeen.Load()
				if n <= m || maxSeen.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(1 * time.Millisecond)
			inflight.Add(-1)
			return tools.Result{Output: parseID(raw)}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	const N = 20
	calls := make([]scriptCall, N)
	for i := 0; i < N; i++ {
		id := "t" + intToA(i)
		calls[i] = scriptCall{id, "Read", `{"id":"` + id + `"}`}
	}
	prov := fake.New(parallelToolScript(calls)...)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	for range agent.Submit(context.Background(), "go") {
	}

	if got := maxSeen.Load(); got > 8 {
		t.Fatalf("semaphore breached: maxSeen=%d want <=8", got)
	}
	if got := maxSeen.Load(); got < 2 {
		t.Fatalf("semaphore never observed >=2 inflight: maxSeen=%d — concurrency not exercised", got)
	}
}

func intToA(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestMixedBatchGroupAndPreserve(t *testing.T) {
	type span struct {
		id           string
		start, end   time.Time
		wasParSafe   bool
	}
	var mu sync.Mutex
	var spans []span

	record := func(id string, parSafe bool) func() {
		mu.Lock()
		s := span{id: id, start: time.Now(), wasParSafe: parSafe}
		spans = append(spans, s)
		idx := len(spans) - 1
		mu.Unlock()
		return func() {
			mu.Lock()
			spans[idx].end = time.Now()
			mu.Unlock()
		}
	}

	readTool := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			id := parseID(raw)
			done := record(id, true)
			time.Sleep(15 * time.Millisecond)
			done()
			return tools.Result{Output: id}, nil
		},
	}
	bashTool := &gateTool{
		name:         "Bash",
		parallelSafe: false,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			id := parseID(raw)
			done := record(id, false)
			time.Sleep(15 * time.Millisecond)
			done()
			return tools.Result{Output: id}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(readTool)
	reg.Register(bashTool)

	calls := []scriptCall{
		{"t1", "Read", `{"id":"t1"}`},
		{"t2", "Read", `{"id":"t2"}`},
		{"t3", "Bash", `{"id":"t3"}`},
		{"t4", "Read", `{"id":"t4"}`},
	}
	prov := fake.New(parallelToolScript(calls)...)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	for range agent.Submit(context.Background(), "go") {
	}

	mu.Lock()
	defer mu.Unlock()
	byID := map[string]span{}
	for _, s := range spans {
		byID[s.id] = s
	}
	if len(byID) != 4 {
		t.Fatalf("expected 4 spans, got %d", len(byID))
	}

	// t1 and t2 must overlap.
	if !byID["t1"].start.Before(byID["t2"].end) || !byID["t2"].start.Before(byID["t1"].end) {
		t.Errorf("t1 and t2 did not overlap: t1=%v..%v t2=%v..%v",
			byID["t1"].start, byID["t1"].end, byID["t2"].start, byID["t2"].end)
	}
	// t3 starts after both t1 and t2 end.
	if !byID["t3"].start.After(byID["t1"].end) || !byID["t3"].start.After(byID["t2"].end) {
		t.Errorf("t3 (Bash) did not wait for t1/t2 to complete")
	}
	// t4 starts after t3 ends.
	if !byID["t4"].start.After(byID["t3"].end) {
		t.Errorf("t4 did not wait for t3 (Bash) to complete")
	}

	// History order preserved.
	var toolResults llm.Message
	for _, m := range agent.history {
		if m.Role == llm.RoleUser && len(m.Content) > 0 && m.Content[0].Type == llm.ContentToolResult {
			toolResults = m
		}
	}
	for idx, c := range calls {
		if toolResults.Content[idx].ToolUseID != c.ID {
			t.Errorf("slot %d: ToolUseID=%q want %q", idx, toolResults.Content[idx].ToolUseID, c.ID)
		}
	}
}

func TestPanicInParallelGroupIsolated(t *testing.T) {
	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			id := parseID(raw)
			if id == "t2" {
				panic("boom")
			}
			return tools.Result{Output: id}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	calls := []scriptCall{
		{"t1", "Read", `{"id":"t1"}`},
		{"t2", "Read", `{"id":"t2"}`},
		{"t3", "Read", `{"id":"t3"}`},
	}
	prov := fake.New(parallelToolScript(calls)...)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	for range agent.Submit(context.Background(), "go") {
	}

	var toolResults llm.Message
	for _, m := range agent.history {
		if m.Role == llm.RoleUser && len(m.Content) > 0 && m.Content[0].Type == llm.ContentToolResult {
			toolResults = m
		}
	}
	if len(toolResults.Content) != 3 {
		t.Fatalf("expected 3 result blocks, got %d", len(toolResults.Content))
	}
	if toolResults.Content[0].IsError {
		t.Errorf("slot 0 (t1) should not be error: %+v", toolResults.Content[0])
	}
	if !toolResults.Content[1].IsError {
		t.Error("slot 1 (t2) should be error (panic)")
	}
	if !strings.Contains(toolResults.Content[1].Output, "panic") {
		t.Errorf("slot 1 output missing panic: %q", toolResults.Content[1].Output)
	}
	if toolResults.Content[2].IsError {
		t.Errorf("slot 2 (t3) should not be error: %+v", toolResults.Content[2])
	}
}

func TestParallelGroupCancellation(t *testing.T) {
	release := make(chan struct{})
	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
			select {
			case <-ctx.Done():
				return tools.Result{Output: "cancelled", IsError: true}, nil
			case <-release:
				return tools.Result{Output: "ok"}, nil
			}
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	calls := []scriptCall{
		{"t1", "Read", `{"id":"t1"}`},
		{"t2", "Read", `{"id":"t2"}`},
		{"t3", "Read", `{"id":"t3"}`},
		{"t4", "Read", `{"id":"t4"}`},
	}
	// Provide only the tool_use script. The follow-up turn after cancellation
	// must see an empty (closed) stream to deterministically produce an
	// ErrorEvent — handing a second end_turn script would race the fake
	// provider's send select against ctx.Done.
	firstScript := parallelToolScript(calls)[0]
	prov := fake.New(firstScript)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	eventsCh := agent.Submit(context.Background(), "go")

	time.Sleep(20 * time.Millisecond)
	agent.CancelCurrent()

	var gotErr bool
	for e := range eventsCh {
		if _, ok := e.(ErrorEvent); ok {
			gotErr = true
		}
	}
	if !gotErr {
		t.Fatal("expected ErrorEvent after cancellation")
	}
	// release intentionally never closed — goroutines must exit via ctx.Done().
	// goleak in TestMain verifies nothing lingers.
}

func TestToolResultEventsAllPair(t *testing.T) {
	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			return tools.Result{Output: parseID(raw)}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	calls := []scriptCall{
		{"t1", "Read", `{"id":"t1"}`},
		{"t2", "Read", `{"id":"t2"}`},
		{"t3", "Read", `{"id":"t3"}`},
		{"t4", "Read", `{"id":"t4"}`},
		{"t5", "Read", `{"id":"t5"}`},
	}
	prov := fake.New(parallelToolScript(calls)...)
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	var toolResults []ToolResult
	for e := range agent.Submit(context.Background(), "go") {
		if tr, ok := e.(ToolResult); ok {
			toolResults = append(toolResults, tr)
		}
	}
	if len(toolResults) != 5 {
		t.Fatalf("expected 5 ToolResult events, got %d", len(toolResults))
	}
	want := map[string]bool{"t1": true, "t2": true, "t3": true, "t4": true, "t5": true}
	for _, tr := range toolResults {
		if !want[tr.ID] {
			t.Errorf("unexpected ToolResult id %q", tr.ID)
		}
		delete(want, tr.ID)
	}
	if len(want) != 0 {
		t.Errorf("missing ToolResult ids: %v", want)
	}
}
