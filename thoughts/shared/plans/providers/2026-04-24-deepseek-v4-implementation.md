# DeepSeek v4 Provider — Implementation Plan

> Date: 2026-04-24
> Design: `thoughts/shared/plans/providers/2026-04-24-deepseek-v4-design.md`
> Wire: Anthropic-compatible (`https://api.deepseek.com/anthropic`)
> Models: `deepseek-v4-pro` (default), `deepseek-v4-flash`
> Defaults: 1M context · 192k max_tokens · 120k thinking budget

## Execution Strategy

Six stages, each with its own verification gate. Each stage can be committed independently. TDD where the code is internal and deterministic (stages 1, 2, 4); integration-style where real wire behavior matters (stage 6).

Stages 1 + 2 are independent of each other (config vs agent plumbing) — could parallelize, but keep serial for simpler review. Stages 3+ depend on 1 + 2.

---

## Stage 1 — Config layer

**Goal:** preset registered, per-model helpers return correct numbers, `ModelConfig.MaxTokens` round-trips from TOML, family prompt refreshed.

### Files

- `internal/config/presets.go` — add `deepseek` entry
- `internal/config/config.go` — `ModelConfig.MaxTokens` field, `ModelContextWindow` branch, `ModelThinkingBudget` branch, new `ModelMaxTokens` helper
- `internal/system/defaults/prompts/deepseek.md` — rewrite for v4 + anthropic wire
- `internal/config/config_test.go` — new cases
- `internal/config/presets_test.go` — new or extend (check existence first)
- `internal/config/family_test.go` — extend for `deepseek-v4-*`

### Changes

**1A. `presets.go`:** add entry to map literal:

```go
"deepseek": {
    Name:         "deepseek",
    Wire:         "anthropic",
    BaseURL:      "https://api.deepseek.com/anthropic",
    APIKeyEnv:    "DEEPSEEK_API_KEY",
    DefaultModel: "deepseek-v4-pro",
    Models:       []string{"deepseek-v4-pro", "deepseek-v4-flash"},
},
```

**1B. `config.go` — `ModelConfig` struct (line 37):** add field.

```go
type ModelConfig struct {
    ContextWindow        int    `toml:"context_window"`
    ReasoningEffort      string `toml:"reasoning_effort"`
    ThinkingBudgetTokens *int   `toml:"thinking_budget_tokens"`
    MaxTokens            int    `toml:"max_tokens"`
}
```

**1C. `config.go` — `ModelContextWindow` (line 64):** replace the legacy deepseek branch

```go
case strings.HasPrefix(name, "deepseek-r"),
    strings.HasPrefix(name, "deepseek-v3"):
    return 131_072
```

with

```go
case strings.HasPrefix(name, "deepseek-v4"):
    return 1_000_000
```

**1D. `config.go` — `ModelThinkingBudget` (line 108):** add case

```go
case strings.HasPrefix(name, "deepseek-v4"):
    return 120_000
```

**1E. `config.go` — new helper** (place directly below `ModelThinkingBudget`):

```go
// ModelMaxTokens returns the per-model output budget. Explicit user
// config wins; otherwise family defaults apply. Returns 0 when no
// family default exists so callers fall back to the global MaxTokens.
func (c *Config) ModelMaxTokens(name string) int {
    if m, ok := c.Models[name]; ok && m.MaxTokens > 0 {
        return m.MaxTokens
    }
    if strings.HasPrefix(name, "deepseek-v4") {
        return 192_000
    }
    return 0
}
```

**1F. `internal/system/defaults/prompts/deepseek.md`** — full rewrite:

```
DEEPSEEK FAMILY.
Anthropic wire. Thinking blocks signed — preserve signature on tool-call round-trip; do not strip ContentThinking.
Context 1M tokens. Max output 384k; default MaxTokens 192k, thinking budget 120k. Long files OK whole.
Thinking mode default, v4 reasons hard — keep hidden reasoning focused.
Tool-call parallelism supported — batch independent reads.
```

### Tests (write first)

**`config_test.go`** — add to existing `ModelContextWindow` / `ModelThinkingBudget` tables, plus a new `TestModelMaxTokens`:

```go
func TestModelMaxTokens(t *testing.T) {
    cfg := &Config{}
    if got := cfg.ModelMaxTokens("deepseek-v4-pro"); got != 192_000 {
        t.Fatalf("pro: got %d want 192000", got)
    }
    if got := cfg.ModelMaxTokens("deepseek-v4-flash"); got != 192_000 {
        t.Fatalf("flash: got %d want 192000", got)
    }
    if got := cfg.ModelMaxTokens("claude-sonnet-4-5"); got != 0 {
        t.Fatalf("unknown: got %d want 0", got)
    }
    cfg.Models = map[string]ModelConfig{
        "deepseek-v4-pro": {MaxTokens: 64_000},
    }
    if got := cfg.ModelMaxTokens("deepseek-v4-pro"); got != 64_000 {
        t.Fatalf("override: got %d want 64000", got)
    }
}
```

**`presets_test.go`** (check existence first via `ls internal/config/presets_test.go`; create if missing):

```go
func TestDeepseekPreset(t *testing.T) {
    p := Presets()
    d, ok := p["deepseek"]
    if !ok { t.Fatal("missing deepseek preset") }
    if d.Wire != "anthropic" { t.Errorf("wire: %q", d.Wire) }
    if d.BaseURL != "https://api.deepseek.com/anthropic" { t.Errorf("base: %q", d.BaseURL) }
    if d.APIKeyEnv != "DEEPSEEK_API_KEY" { t.Errorf("env: %q", d.APIKeyEnv) }
    if d.DefaultModel != "deepseek-v4-pro" { t.Errorf("default: %q", d.DefaultModel) }
    want := []string{"deepseek-v4-pro", "deepseek-v4-flash"}
    if !reflect.DeepEqual(d.Models, want) { t.Errorf("models: %v", d.Models) }
}
```

**`family_test.go`** — extend existing `FamilyForModel` table with `deepseek-v4-pro` → `deepseek`, `deepseek-v4-flash` → `deepseek`.

### Gate

```bash
go test ./internal/config/... ./internal/system/...
```

Expected: all pass, new cases included.

---

## Stage 2 — Agent per-turn MaxTokens resolver

**Goal:** `agent.Options.MaxTokensResolverFn` threads through `Agent.turn()` so the correct per-model cap is used every turn, including after `SetModel`.

### Files

- `internal/agent/agent.go` — `Options` field, `Agent` field, `New` wiring
- `internal/agent/loop.go` — per-turn resolve at request-build site (line 33)
- `internal/agent/loop_parallel_test.go` OR new `loop_maxtokens_test.go` — new unit tests (the repo already has parallel test scaffolding per `loop_parallel_test.go`)

### Changes

**2A. `agent.go` `Options` struct (line 45):**

```go
type Options struct {
    ...existing fields...
    MaxTokensResolverFn func(model string) int
}
```

**2B. `agent.go` `Agent` struct (around line 40):**

```go
type Agent struct {
    ...existing fields...
    maxTokensResolver func(string) int
}
```

**2C. `agent.go` `New` (around line 83):** assign in constructor literal:

```go
a := &Agent{
    ...
    maxTokensResolver: opts.MaxTokensResolverFn,
}
```

**2D. `loop.go` line 33** — change the Request build from

```go
MaxTokens: a.maxTokens,
```

to (read current model under mutex, resolve, fall back):

```go
a.mu.Lock()
model := a.model
resolver := a.maxTokensResolver
defaultMax := a.maxTokens
a.mu.Unlock()
maxTokens := defaultMax
if resolver != nil {
    if v := resolver(model); v > 0 {
        maxTokens = v
    }
}
// ...inside Request literal...
MaxTokens: maxTokens,
```

(Adjust for whatever local `model` var already exists in that function — keep the mutex scope narrow.)

### Tests (write first)

**`loop_maxtokens_test.go`** — four cases using a fake `llm.Provider` that records the last `Request.MaxTokens`:

```go
func TestAgentMaxTokens_UsesResolverWhenPositive(t *testing.T) {
    // resolver returns 192_000 → Request.MaxTokens == 192_000 even
    // though opts.MaxTokens is 32_768.
}

func TestAgentMaxTokens_FallsBackWhenResolverReturnsZero(t *testing.T) {
    // resolver returns 0 → Request.MaxTokens == opts.MaxTokens.
}

func TestAgentMaxTokens_FallsBackWhenResolverNil(t *testing.T) {
    // no resolver set → Request.MaxTokens == opts.MaxTokens.
}

func TestAgentMaxTokens_ResolverFiresPerTurn(t *testing.T) {
    // Construct with Model="claude-sonnet-4-5" and a resolver map:
    //   claude-sonnet-4-5 -> 0 (fallback)
    //   deepseek-v4-pro   -> 192_000
    // Run a turn → 32_768. SetModel("deepseek-v4-pro"). Run another turn → 192_000.
    // Proves the resolver is consulted per turn, not cached at construction.
}
```

Use the existing fake-provider test scaffolding pattern from `loop_parallel_test.go` — look at how it intercepts `llm.Provider.Generate` calls.

### Gate

```bash
go test ./internal/agent/...
```

Expected: new cases green, existing tests untouched.

---

## Stage 3 — Wire resolver from main.go

**Goal:** both `agent.New` call sites in `cmd/sam/main.go` receive the `MaxTokensResolverFn` backed by `cfg.ModelMaxTokens`. TUI unchanged (agent isn't rebuilt on model/provider switch; per-turn resolver handles live switches).

### Files

- `cmd/sam/main.go` lines 176–187 (TUI path) and 242–250 (one-shot path) — add option

### Changes

**3A. TUI path (main.go:176):**

```go
a := agent.New(agent.Options{
    Provider:            prov,
    Tools:               registry,
    Policy:              pol,
    System:              sys,
    Model:               cfg.Model,
    MaxIters:            cfg.MaxIterations,
    MaxTokens:           cfg.MaxTokens,
    MaxTokensResolverFn: func(m string) int { return cfg.ModelMaxTokens(m) },
    LaunchDir:           cwd,
    Logger:              logger,
    Skills:              skillReg,
})
```

**3B. One-shot path (main.go:242):** same addition.

### Gate

```bash
go build ./...
```

Expected: clean build. No behavioral change for existing providers (resolver returns 0 for non-deepseek models → fallback to `cfg.MaxTokens`).

---

## Stage 4 — Wire-boundary tests (anthropiccompat)

**Goal:** catch future refactors of `llm.Request.MaxTokens` / `ThinkingBudgetTokens` that silently break the wire. Assert outbound JSON contains the expected numbers.

### Files

- `internal/llm/anthropiccompat/client_test.go` — extend with a fake `http.RoundTripper`

### Approach

The Anthropic SDK accepts an `option.WithHTTPClient(&http.Client{Transport: rt})`. In the test, intercept the outbound request, buffer the body, and assert on the JSON. Streaming response can be a minimal SSE that ends the stream cleanly (a `message_stop` event).

### Tests

```go
func TestAnthropicCompat_MaxTokensAndThinkingInBody(t *testing.T) {
    // Arrange: RoundTripper records req body.
    // Act: client.Stream with Request{Model:"deepseek-v4-pro", MaxTokens:192_000, ThinkingBudgetTokens:120_000, Messages:[user:"hi"]}.
    // Assert body JSON:
    //   "max_tokens": 192000
    //   "thinking": { "type": "enabled", "budget_tokens": 120000 }
}

func TestAnthropicCompat_ThinkingBudgetZeroOmitsBlock(t *testing.T) {
    // Same harness, ThinkingBudgetTokens = 0.
    // Assert body JSON does NOT contain a "thinking" key at all.
}
```

Model name in the test can stay `deepseek-v4-pro` to make the intent clear, though the client is model-agnostic.

### Gate

```bash
go test ./internal/llm/anthropiccompat/...
```

Expected: both cases green. Existing client tests unaffected.

---

## Stage 5 — Docs + index refresh

**Goal:** users can discover the provider from README; codebase index reflects the new preset + helper.

### Files

- `README.md` — provider table row + secrets.toml example + defaults note
- `thoughts/shared/index/providers.md` — append `deepseek` to preset enumeration
- `thoughts/shared/index/config.md` — mention `ModelMaxTokens` alongside `ModelContextWindow` / `ModelThinkingBudget`

### Changes

**5A. `README.md` providers table** — add row:

```
| deepseek | anthropic | deepseek-v4-pro | DEEPSEEK_API_KEY |
```

Append to secrets.toml example:

```toml
DEEPSEEK_API_KEY = "..."
```

Add a short paragraph near the provider defaults section:

```
**DeepSeek v4:** 1M context, 192k max_tokens, 120k thinking budget by default.
Override per-model under `[models.<name>]` (e.g. `max_tokens = 64000`).
```

**5B. `thoughts/shared/index/providers.md`** — edit the "preset" sentence to include `deepseek`.

**5C. `thoughts/shared/index/config.md`** — add `ModelMaxTokens` to the helpers list.

### Gate

Human skim.

---

## Stage 6 — Manual smoke against real API

**Goal:** prove the wire works end-to-end with a real `DEEPSEEK_API_KEY` (already in repo `.env`).

### Steps

1. Source the key: `set -a; source .env; set +a` (or whatever the project's loader does — check `cmd/sam/main.go` for `.env` handling; fall back to shelling `export`).
2. One-shot run:
   ```bash
   SAM_PROVIDER=deepseek SAM_MODEL=deepseek-v4-pro \
     go run ./cmd/sam -p "Read README.md and summarize in 3 bullets."
   ```
   - Verify: thinking blocks stream, tool call `read` fires, response completes cleanly (no `stop_reason: max_tokens`).
3. Flash smoke:
   ```bash
   SAM_PROVIDER=deepseek SAM_MODEL=deepseek-v4-flash \
     go run ./cmd/sam -p "What is 2+2?"
   ```
   - Verify: responds quickly, no thinking-signature round-trip errors.
4. TUI mid-session switch: launch `go run ./cmd/sam`, start on default (claude/minimax/whatever), open settings modal, switch to `deepseek` provider + `deepseek-v4-pro` model, issue a new prompt requiring a tool call. Verify thinking streams and completes under the 192k cap.
5. (Optional) Override test: add to `~/.config/sam/config.toml`:
   ```toml
   [models."deepseek-v4-pro"]
   max_tokens = 32000
   ```
   Re-run step 2. Verify long responses truncate (proving override path works).

### Gate

All four steps behave as expected. If not, debug before declaring feature complete.

---

## Post-Implementation

- Update `thoughts/shared/index/CODEBASE-MAP.md` last-updated line.
- Run `caveroach:update-codebase-index` to capture the new preset + `ModelMaxTokens` + `MaxTokensResolverFn` surfaces in the persistent index.
- Commit per stage (stage 1, stage 2, stage 3, stage 4, stage 5 → one commit each; stage 6 is verification, no commit unless fixes found).

## Risk Checklist

- [ ] `/anthropic` endpoint on DeepSeek supports SSE streaming identically to Anthropic (if not, smoke fails in stage 6 → fall back to OpenAI wire, roll the design).
- [ ] DeepSeek signs thinking blocks so `anthropic.NewThinkingBlock(signature, text)` round-trip works in tool-use continuations (if unsigned, strip ContentThinking on the way out — guard already exists in `anthropiccompat/client.go:35`).
- [ ] User config with explicit `[models."deepseek-v4-pro"] thinking_budget_tokens = 0` disables thinking cleanly (covered by stage 4 test).
- [ ] Global `MaxTokens = 32768` default not accidentally overridden for non-deepseek models (covered by stage 2 fallback test).
