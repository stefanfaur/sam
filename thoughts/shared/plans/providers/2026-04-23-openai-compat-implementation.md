# OpenAI-Compatible Provider Support — Implementation Plan

**Status:** Ready
**Date:** 2026-04-23
**Design spec:** [`2026-04-22-openai-compat-design.md`](./2026-04-22-openai-compat-design.md) — source of truth. Read first. All locked decisions (Q1–Q8, S/P, R/E, M/U) live in the decisions table at the top of that doc; every task below cites the relevant section.
**Target branch:** feature branch off `main` — `feat/openai-compat`.

## How to use this plan

- Tasks are numbered. Each task lists: **files**, **steps**, **verification** commands, and **commit message**.
- Tasks 1–12 form the critical path; 13–15 (TUI) and 16–18 (tests) can run in parallel with 6 onward.
- Task 0 (§0 probe) blocks only Trinity-specific steps (4.Trinity transcript and 6).
- One task = one commit. Pre-commit hooks stay on (`gofmt`, tests); fix failures, re-stage, new commit — never `--amend`.
- TDD where the task involves logic: write tests first, make them pass, then move on.

---

## Task 0 — §0 Pre-Implementation Probe (partial blocker)

**Blocks:** Task 4 Trinity SSE fixture; Task 6 Arcee preset capability row; Task 12 live Arcee smoke test.
**Does not block:** Anything else.

**Steps:**

1. Obtain an Arcee Conductor API key. Export `ARCEE_API_KEY`.
2. Probe model slug + wire shape:
   ```bash
   curl -sN https://conductor.arcee.ai/v1/chat/completions \
     -H "Authorization: Bearer $ARCEE_API_KEY" \
     -H "Content-Type: application/json" \
     -d '{"model":"trinity-large-thinking","stream":true,
           "messages":[{"role":"user","content":"Say hi and briefly think first."}]}'
   ```
3. Record in a followup edit to the design doc:
   - Working slug (fall back to `trinity-thinking-large`, `Trinity-Large-Thinking` if the first 400s).
   - Which delta field carries reasoning: `delta.reasoning_content`, `delta.reasoning`, or inline `<think>` in `delta.content`.
   - Whether `stream_options.include_usage` is honored (rerun with `"stream_options":{"include_usage":true}` added).
4. Commit the spec update.

**Verification:** `git diff thoughts/shared/plans/providers/2026-04-22-openai-compat-design.md` shows the probe result captured in a new "§0 Probe Results (2026-04-23)" subsection.

**Commit:** `docs(providers): record Arcee Conductor probe results`

---

## Task 1 — Config refactor: map-based providers + presets

**Spec:** §1, §2.
**Files:**
- `internal/config/config.go` (rewrite `Config`, add `ProviderEntry`, `CapsOverride`, `ModelConfig`)
- `internal/config/presets.go` (new — hardcoded preset map)
- `internal/config/config_test.go` (extend)

**Steps:**

1. Replace the nested `Config.Providers` struct with `Providers map[string]ProviderEntry`. Field keys: `Name`, `Wire`, `BaseURL`, `APIKeyEnv`, `DefaultModel`, `Models`, `ModelPrefixes`, `ParseThinkTags`, `Caps CapsOverride`.
2. Add `CapsOverride` with all fields listed in §4 of the spec (`SystemRole`, `SystemRoleFallback`, `MaxTokensField`, `SupportsSamplingParams`, `SupportsReasoningEffort`, `SupportsIncludeUsage`, `SupportsParallelToolCalls`, `EchoReasoning`, `ReasoningSource`, `AuthHeader`, all `*` pointers).
3. Extend `ModelConfig` with `ReasoningEffort string`.
4. Create `internal/config/presets.go` exporting `Presets() map[string]ProviderEntry` returning hardcoded entries for `minimax`, `anthropic`, `openai`, `arcee` (values from spec §2; Arcee `ParseThinkTags=false` pending Task 0).
5. In `Load()`:
   - Start with `cfg.Providers = Presets()`.
   - After TOML unmarshal, for every key present in user TOML, **replace** the entry entirely (full-replace semantics, spec §2). Fill `Name` field from map key.
   - Validate: each entry's `Wire` must be `"anthropic"` or `"openai"`; `cfg.Provider` must be a known key.
6. Extend `ModelContextWindow` with prefix rules for `gpt-5`/`o1`/`o3`/`o4` → 400000, `gpt-4o`/`gpt-4.1` → 128000, `deepseek-r`/`deepseek-v3` → 131072, `trinity-` → 512000.
7. Remove `SAM_PROVIDER` hard-coded allow-list check; just validate against the merged map.
8. Tests:
   - Presets load on empty TOML.
   - User TOML `[providers.openai]` with only `base_url` replaces the whole preset entry (does NOT keep preset's `default_model`).
   - New key `[providers.groq]` appends.
   - Unknown wire errors.
   - `cfg.Provider = "bogus"` errors.
   - `[models."gpt-5"] reasoning_effort = "high"` resolves.

**Verification:** `go test ./internal/config/...`

**Commit:** `refactor(config): map-based providers with presets + full-replace merge`

---

## Task 2 — Secrets refactor: `[api_keys]` map

**Spec:** §2 (S2).
**Files:** `internal/config/secrets.go`, new `internal/config/secrets_test.go`.

**Steps:**

1. Replace `Secrets` struct with `Secrets struct { APIKeys map[string]string \`toml:"api_keys"\` }`.
2. `ApplyEnv(cfg *Config)` iterates `cfg.Providers`; for each `name, entry`, if `s.APIKeys[name] != ""` and `os.Getenv(entry.APIKeyEnv) == ""`, set the env var.
3. `SaveSecrets(s Secrets)` writes the new shape.
4. Legacy hardcoded fields (`minimax_api_key`, `anthropic_api_key`) removed; old TOML silently ignored (M3 breaking change — documented in README later).
5. Update `cmd/sam/main.go`: `config.LoadSecrets().ApplyEnv(cfg)` takes the `cfg` now.
6. Tests: `ApplyEnv` respects existing env; missing entry key is a no-op; overlapping keys don't overwrite.

**Verification:** `go test ./internal/config/...`

**Commit:** `refactor(config): secrets as [api_keys] map keyed by provider name`

---

## Task 3 — Add `ContentThinking` + agent loop accumulator

**Spec:** §7 #1, #2.
**Files:** `internal/llm/types.go`, `internal/agent/loop.go`, `internal/agent/agent_test.go`.

**Steps:**

1. `types.go`: add `ContentThinking ContentType = "thinking"`. No new struct fields — `Text` carries the reasoning.
2. `loop.go` around line 90 (`case llm.EventThinkingDelta`): replace the fire-and-forget emit with an accumulator that also appends to `msg.Content`, mirroring the text-block logic at `loop.go:95-102`. Collapse consecutive thinking deltas into one `ContentThinking` block.
3. Test: fake provider emits `EventThinkingDelta` twice, then `EventMessageStop`. Assert `agent.history[last].Content` contains a single `ContentThinking` block with concatenated text. Assert ordering preserved across text/thinking/tool_use interleaving.

**Verification:** `go test ./internal/agent/...`

**Commit:** `feat(agent): accumulate thinking deltas into ContentThinking history blocks`

---

## Task 4 — Think-tag parser (`openaicompat/thinktag.go`)

**Spec:** §4 think-tag section.
**Files:** new `internal/llm/openaicompat/thinktag.go`, new `internal/llm/openaicompat/thinktag_test.go`.

**Steps (TDD — tests first):**

1. Write table-driven test covering every case in spec §4:
   - `hello world` → text/empty.
   - `hi <think>ok</think>bye` → `hi bye`/`ok`.
   - `<think>a</think><think>b</think>` → ``/`ab`.
   - `pre<think>tail` (unclosed) → `pre`/`tail`.
   - `drink <thirsty> water` → `drink <thirsty> water`/``.
   - `<<think>x</think>` → `<`/`x`.
   - 2-chunk `<` + `<think>x</think>` → `<`/`x`.
   - `<think>a<think>b</think>c` → `c`/`a<think>b`.
2. For each case, also run with chunk splits at every byte boundary 1..N, asserting byte-identical output regardless of split. Helper: `feedAllSplits(input string) (text, thinking string)`.
3. Implement `Parser` with `state State`, `carry []byte`, and methods `Feed(chunk string) (text, thinking string)` and `Flush() (text, thinking string)`.
4. Implement peek-then-append semantics (spec §4): `MAYBE_OPEN` and `MAYBE_CLOSE` check whether `carry + nextByte` is still a strict prefix (or full match) of the target tag **before** appending; divergence flushes current carry and reprocesses the diverging byte from `OUTSIDE`/`INSIDE`.
5. Make tests green.

**Verification:** `go test ./internal/llm/openaicompat/...`

**Commit:** `feat(openaicompat): streaming <think> tag parser with peek-then-append state machine`

---

## Task 5 — Capability table (`openaicompat/caps.go`)

**Spec:** §4 Capabilities struct + table.
**Files:** new `internal/llm/openaicompat/caps.go`, new `internal/llm/openaicompat/caps_test.go`.

**Steps:**

1. Define `Capabilities` struct with all fields from spec §4 (`SystemRole`, `SystemRoleFallback`, `MaxTokensField`, `SupportsSamplingParams`, `SupportsReasoningEffort`, `SupportsIncludeUsage`, `SupportsParallelToolCalls`, `EchoReasoning`, `ReasoningSource`, `AuthHeader`).
2. `defaultCaps(model string) Capabilities` — longest-prefix match over the table:

   | Prefix | Role | MaxTokens | Sampling | Effort | Source | Echo |
   |---|---|---|---|---|---|---|
   | `gpt-5`,`o1`,`o3`,`o4` | developer | max_completion_tokens | false | true | none | false |
   | `deepseek-r` | system | max_tokens | true | false | reasoning_content | true |
   | `trinity-large-thinking` | system | max_tokens | true | false | (per Task 0 probe) | true |
   | `gpt-4`,`gpt-4o`,`gpt-3.5` | system | max_tokens | true | false | none | false |
   | _default_ | system | max_tokens | true | false | none | false |

   All defaults: `SupportsIncludeUsage=true`, `SupportsParallelToolCalls=true`, `AuthHeader="bearer"`.
3. `MergeCaps(base Capabilities, override config.CapsOverride) Capabilities` — each non-nil override field replaces the base.
4. Tests: prefix match wins over default; override order is model > entry > prefix (this is caller's responsibility — test the two-arg merge directly); `SupportsSamplingParams=false` drops all four sampling fields semantically (verified when request builder consumes this).

**Verification:** `go test ./internal/llm/openaicompat/...`

**Commit:** `feat(openaicompat): capability table with prefix-based defaults + overrides`

---

## Task 6 — Wire structs (`openaicompat/wire.go`)

**Spec:** §3.5 `chatRequest` / `chatMessage` / `streamChunk`.
**Files:** new `internal/llm/openaicompat/wire.go`.

**Steps:**

1. Transcribe the struct definitions from spec §3.5: `chatRequest`, `chatMessage` (with `Content *string`, `Reasoning string`, `ToolCalls`, `ToolCallID`), `chatTool`, `chatToolCall`, `streamOptions`.
2. Add response/stream side: `streamChunk { ID, Object, Created int, Model, SystemFingerprint string; Choices []streamChoice; Usage *streamUsage }`. `streamChoice { Index int; FinishReason *string; Delta streamDelta }`. `streamDelta { Role, Content *string, Refusal *string, ReasoningContent, Reasoning string, ToolCalls []streamToolCallDelta }`. `streamToolCallDelta { Index int; ID, Type string; Function struct { Name, Arguments string } }`. `streamUsage { PromptTokens, CompletionTokens int; CompletionTokensDetails struct { ReasoningTokens int } }`.
3. No logic yet — just types + JSON tags.

**Verification:** `go build ./internal/llm/openaicompat/...`

**Commit:** `feat(openaicompat): wire request/response struct definitions`

---

## Task 7 — SSE reader (`openaicompat/sse.go`)

**Spec:** §3.5 step 5.
**Files:** new `internal/llm/openaicompat/sse.go`, new `internal/llm/openaicompat/sse_test.go`.

**Steps (TDD):**

1. Tests first:
   - Valid frame: `data: {"x":1}\n\n` → one decoded chunk.
   - Comment line `: heartbeat\n` → ignored.
   - `data: [DONE]\n\n` → scanner returns `io.EOF`-equivalent sentinel.
   - Multi-line `data:` fields (two `data:` lines in one frame, joined with `\n`).
   - Malformed JSON → error returned.
   - Abrupt EOF mid-frame → error.
2. Implement a `scanner` wrapping `bufio.Scanner` with increased buffer (≥1MB — GPT responses can be long). Method: `Next() (raw []byte, done bool, err error)`. Decoder in `stream.go` unmarshals `raw` into `streamChunk`.

**Verification:** `go test ./internal/llm/openaicompat/...`

**Commit:** `feat(openaicompat): minimal SSE frame reader`

---

## Task 8 — Message translation (`openaicompat/translate.go`)

**Spec:** §3 message translation rules.
**Files:** new `internal/llm/openaicompat/translate.go`, new `internal/llm/openaicompat/translate_test.go`.

**Steps (TDD):**

1. Tests first — cases from spec §6 test matrix:
   - User-text only → one `{role:"user", content:"..."}`.
   - User with 2 `ContentToolResult` blocks (one success, one error) → two `{role:"tool", tool_call_id, content}` messages; error wrapped as `{"error":true,"output":"..."}` JSON.
   - Assistant text+tool_use → single assistant message with `content` + `tool_calls`.
   - Assistant thinking+tool_use, no text, `EchoReasoning=true` → `{role:"assistant", reasoning:"...", tool_calls:[...]}` with **`content` omitted** (not empty string; verify with JSON marshal containing no `"content"` key).
   - Assistant with thinking but `EchoReasoning=false` → thinking dropped.
   - Tool schema with explicit `required` array → preserved verbatim. Schema without `required` → field not synthesized (fixes `anthropiccompat` bug).
   - Empty `req.Tools` → output has no `tools` and no `tool_choice` field.
   - System role drives from `caps.SystemRole` (test `"system"` and `"developer"` branches).
2. Implement `toWireMessages(sys string, msgs []llm.Message, caps Capabilities) []chatMessage`. Implement `toWireTools(tools []llm.ToolDef) []chatTool`. Implement `buildRequest(req llm.Request, caps Capabilities, effort string) chatRequest` wiring the full request-param table from spec §3.
3. `parallel_tool_calls` emission: only set when `caps.SupportsParallelToolCalls == false` (then emit `false`), otherwise leave `nil`.

**Verification:** `go test ./internal/llm/openaicompat/...`

**Commit:** `feat(openaicompat): llm.Request → wire chatRequest translation`

---

## Task 9 — Stream translator (`openaicompat/stream.go`)

**Spec:** §3 stream translation rules.
**Files:** new `internal/llm/openaicompat/stream.go`, new `internal/llm/openaicompat/stream_test.go`.

**Steps (TDD):**

1. Capture canned SSE transcripts under `internal/llm/openaicompat/testdata/` (synthesize from spec, don't require live API):
   - `gpt4o_plain.sse` — simple text completion.
   - `gpt5_reasoning_hidden.sse` — text + usage with `reasoning_tokens` set, no reasoning deltas.
   - `deepseek_r1_reasoning_content.sse` — reasoning via `delta.reasoning_content`.
   - `trinity_reasoning.sse` — pending Task 0 (shape chosen from probe result).
   - `parallel_tool_calls.sse` — two tool calls interleaved by index.
   - `refusal.sse` — `delta.refusal` non-empty.
   - `finish_with_content.sse` — `finish_reason` in the same chunk as final `delta.content`.
   - `missing_finish_reason.sse` — clean `[DONE]` without `finish_reason`.
   - `tool_id_once.sse` — tool-call `id` only on first chunk for its index.
   - `invalid_tool_json.sse` — `finish_reason=tool_calls` with malformed `function.arguments`.
   - `thinking_plus_tool_no_content.sse` — assistant emits reasoning then tool call, no text.
2. Test: for each transcript, assert exact `[]llm.StreamEvent` sequence.
3. Implement `translate(chunks <-chan streamChunk, caps Capabilities, tagParser *thinktag.Parser) <-chan llm.StreamEvent` with the order rules from spec §3 (`content → reasoning → tool_calls → refusal → finish_reason`), tool-call accumulator keyed by index, `json.Valid` guard before emitting stop, finish_reason mapping table.

**Verification:** `go test ./internal/llm/openaicompat/...`

**Commit:** `feat(openaicompat): stream chunks → llm.StreamEvent translation with accumulator`

---

## Task 10 — HTTP client + provider glue (`openaicompat/client.go`, `provider.go`)

**Spec:** §3.5 HTTP path.
**Files:** new `internal/llm/openaicompat/client.go`, new `internal/llm/openaicompat/provider.go`.

**Steps:**

1. `Client` struct: `apiKey`, `baseURL`, `caps Capabilities`, `effortResolver func(string) string`, `httpClient *http.Client` (uses `http.DefaultTransport`).
2. `Stream(ctx, req)`:
   - Resolve caps = `MergeCaps(defaultCaps(req.Model), entry.Caps)` (entry merge done by `provider.go`; client receives merged caps).
   - Build `chatRequest` via `translate.buildRequest`; marshal to JSON.
   - `http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bodyReader)`.
   - Headers per `caps.AuthHeader`: `bearer` → `Authorization: Bearer <key>`; `api-key` → `api-key: <key>`; `none` → no auth header. Plus `Content-Type: application/json`, `Accept: text/event-stream`, `Accept-Encoding: identity`.
   - No `http.Client.Timeout` (streaming).
   - On non-200: read body (cap 64KB), try decode `{"error":{"message","type","code"}}`, emit single `EventError` wrapping `fmt.Errorf("openai %d %s: %s", status, errType, message)` (include `Retry-After` for 429), close body, return channel with that one event then close.
   - On 200: spawn goroutine running `sse.Scanner` → `streamChunk` decoder → `stream.translate` → forwarding to the returned `chan llm.StreamEvent`. Bind the output channel's close to both `[DONE]` and `ctx.Done()`.
3. `Provider` struct implements `llm.Provider`: `Name() string` (= entry name), `Stream` delegates to client.
4. `New(opts Options) (*Provider, error)` where `Options{Entry config.ProviderEntry, Model string, EffortResolver func(string) string}`. Reads API key from `os.Getenv(entry.APIKeyEnv)`; returns error if empty and `AuthHeader != "none"`.

**Verification:** `go build ./internal/llm/openaicompat/...`; no new test here — covered by Task 9 fixtures + Task 12 live smoke.

**Commit:** `feat(openaicompat): HTTP client + provider implementing llm.Provider`

---

## Task 11 — Registry dispatcher + main.go wiring

**Spec:** §1 dispatcher.
**Files:** new `internal/llm/registry.go`, `cmd/sam/main.go`.

**Steps:**

1. `internal/llm/registry.go` exports `Build(entry config.ProviderEntry, model string, resolver func(string) string) (Provider, error)` switching on `entry.Wire`; returns `anthropiccompat.NewProvider(entry, model)` or `openaicompat.New(...)`.
2. `cmd/sam/main.go`:
   - Replace the hardcoded `switch name` in `buildProvider` with a call to `llm.Build`.
   - Resolver: `func(m string) string { return cfg.Models[m].ReasoningEffort }`.
   - `mustProvider(cfg)` looks up `cfg.Providers[cfg.Provider]` and passes the entry.
3. `anthropiccompat` gets a new `NewProvider(entry config.ProviderEntry, model string) (*Provider, error)` shim that reads `entry.BaseURL`, `entry.APIKeyEnv` and constructs the existing `Client`.
4. Delete the `minimax` and `anthropic` sub-packages' `New` entrypoints are **not** deleted here — they stay until Task 14 so anthropicompat tests keep compiling.

**Verification:** `go build ./...`; existing `go test ./...` all green (Anthropic wire behavior unchanged).

**Commit:** `feat(llm): registry dispatcher + config-driven provider build`

---

## Task 12 — Anthropic wire fixes (drop thinking, schema required)

**Spec:** §7 #3, §3 Tool schema translation.
**Files:** `internal/llm/anthropiccompat/client.go`, `internal/llm/anthropiccompat/client_test.go` (add if missing).

**Steps:**

1. In message translation loop (around `anthropiccompat/client.go:62`), skip `ContentThinking` blocks silently — no wire emission.
2. Fix the `required` bug at lines ~94–99: instead of collecting every property name into `required`, read `schema["required"]` as `[]any` if present, coerce to `[]string`, else leave empty.
3. Test: tool with explicit `required: ["a"]` round-trips that exact list; tool without `required` produces empty list.
4. Test: assistant message with `ContentThinking` block does not appear in the outbound Anthropic request.

**Verification:** `go test ./internal/llm/anthropiccompat/...`

**Commit:** `fix(anthropiccompat): preserve schema required; drop ContentThinking pre-wire`

---

## Task 13 — `/provider` and `/model` commands (R3 routing)

**Spec:** §5 R3.
**Files:** `internal/tui/` — exact filenames to discover (grep for existing `/provider` handler).

**Steps:**

1. Find the current `/provider` and `/model` command handlers via `rg "/provider|/model" internal/tui`.
2. `/provider <name>`: validate `<name>` is a key in `cfg.Providers`; on miss, render error listing all known providers. On hit, call `ProviderFactory(name, cfg.Providers[name].DefaultModel)`.
3. `/model <spec>`:
   - If `<spec>` contains `/`, split on first `/` — provider + model. Validate provider exists, switch + set model.
   - Otherwise bare model name: scan all `cfg.Providers` entries. Match priority: exact `entry.DefaultModel`, then `entry.Models` membership, then `entry.ModelPrefixes` match (also check the hardcoded prefix table in `openaicompat/caps.go` via an exported helper `caps.MatchesPrefix(model) bool`).
   - Zero matches → render "unknown model; try `provider/model`".
   - Multiple → render "ambiguous; matches: [a, b]; use `a/<model>`".
   - One → switch silently.
4. Status bar: render as `<provider>/<model>` unconditionally.
5. Tests: exercise command parsing in a TUI test if infra exists, else a unit test on the resolver helper.

**Verification:** `go build ./...`; manual: start TUI, try `/provider minimax`, `/model openai/gpt-4o-mini`, `/model gpt-4o-mini`.

**Commit:** `feat(tui): /provider + /model with R3 hybrid routing`

---

## Task 14 — `/auth` rewrite + settings Providers tab

**Spec:** §5 U1.
**Files:** `internal/tui/` handler files for `/auth` + settings modal (commit `e3335b3` added the tabbed modal — locate via `rg "/auth|settings" internal/tui`).

**Steps:**

1. `/auth` no arg: render a table with one row per `cfg.Providers` entry — columns: name, wire, api_key_env, key-status (`✓` if `os.Getenv(entry.APIKeyEnv) != ""`, else `✗`).
2. `/auth <name>`: validate name exists. Prompt for key via existing input row. On submit, write to `secrets.toml`'s `[api_keys] <name> = "..."` using `config.SaveSecrets`, then call `ApplyEnv`, then rebuild the active provider if `<name>` matches `cfg.Provider`.
3. Settings modal: add a new "Providers" tab rendering the same read-only table as `/auth` (reuse the component). Edits happen via config file — YAGNI.
4. Manual test + any existing TUI unit tests.

**Verification:** `go build ./...`; manual TUI walkthrough.

**Commit:** `feat(tui): /auth map iteration + providers settings tab`

---

## Task 15 — Cleanup deprecated provider packages

**Files:** `internal/llm/anthropic/`, `internal/llm/minimax/` (delete), imports in `cmd/sam/main.go`.

**Steps:**

1. Confirm via `rg "internal/llm/(anthropic|minimax)" --type go` that the only importers are `cmd/sam/main.go`.
2. Remove those imports from `main.go` (already unused after Task 11).
3. `git rm -r internal/llm/anthropic internal/llm/minimax`.
4. `go build ./...` and `go test ./...`.

**Verification:** `go build ./...`; `go test ./...` green.

**Commit:** `refactor(llm): remove anthropic/minimax subpackages (now presets)`

---

## Task 16 — Multi-turn reasoning preservation test

**Spec:** §7 full round-trip.
**Files:** `internal/agent/agent_test.go`.

**Steps:**

1. Fake provider that on the first call emits `EventThinkingDelta{"reasoning body"}` + `EventTextDelta{"text body"}` + `EventMessageStop{StopReason:"end_turn"}`.
2. On the second call, capture the `llm.Request.Messages` it receives. Assert the prior assistant message has a `ContentBlock{Type: ContentThinking, Text: "reasoning body"}` present.
3. Run a second test where agent policy allows a tool call on turn 1, assert the tool_use round-trip preserves the preceding thinking block ordering.

**Verification:** `go test ./internal/agent/...`

**Commit:** `test(agent): multi-turn reasoning preservation round-trip`

---

## Task 17 — Live smoke tests (env-gated)

**Spec:** §6 Smoke tests.
**Files:** new `internal/llm/openaicompat/integration_test.go` (build tag `integration`, and/or `t.Skip` when env unset — match `anthropic/integration_test.go` style).

**Steps:**

1. `TestProviderLiveOpenAI`: `t.Skip` unless `OPENAI_API_KEY` set. Call `gpt-4o-mini` with a tiny prompt, assert at least one `TextDelta` and one `MessageStop` arrive.
2. `TestProviderLiveOpenAIReasoning`: same, model `gpt-5-mini` (or `o4-mini` if available), with `reasoning_effort = "minimal"` via a test-local `effortResolver`. Assert usage includes non-zero output tokens.
3. `TestProviderLiveArcee`: skip unless `ARCEE_API_KEY`. Slug + field shape from Task 0. Assert at least one `ThinkingDelta` and one `TextDelta`.
4. `TestMultiTurnReasoningPreservationLive`: optional — 2-turn Arcee call, assert the second request body (captured via test doubles around `http.Transport`) contains the first turn's reasoning echoed back.

**Verification:** `OPENAI_API_KEY=... go test -tags integration ./internal/llm/openaicompat/`.

**Commit:** `test(openaicompat): live smoke tests for openai + arcee wire`

---

## Task 18 — Full validation + README

**Files:** `README.md`.

**Steps:**

1. `go build ./cmd/sam`.
2. `go test ./...`.
3. `gofmt -l .` clean.
4. Rewrite README sections:
   - Example `config.toml` with map-based providers.
   - `secrets.toml` using `[api_keys]`.
   - Migration note (M3 breaking): point users at the spec doc for old → new field mapping.
   - New commands: `/provider`, `/model`, `/auth` behavior; `openai/gpt-4o-mini` model spec form.
   - New env vars: `OPENAI_API_KEY`, `ARCEE_API_KEY`.
   - `[models."gpt-5"] reasoning_effort = "medium"` example.
   - Short note on `parse_think_tags` for self-hosted reasoning models without a server-side parser.

**Verification:** Binary launches; `sam -p "hello"` works against every preset (manual); no gofmt/vet warnings.

**Commit:** `docs(readme): config + command reference for multi-provider support`

---

## Dependency graph

```
Task 0 (probe) ──┐
                 ├── Task 4 Trinity fixture, Task 12 Arcee smoke
Task 1 (config) ─┬── Task 2 (secrets) ─── Task 11 (registry)
                 │                              │
                 └── Task 3 (ContentThinking) ──┼── Task 10 (client+provider)
                                                │
Task 5 (caps) ──┬── Task 8 (translate) ────────┤
Task 6 (wire) ──┘         │                     │
Task 4 (thinktag) ───── Task 9 (stream) ────────┘
Task 7 (sse) ─────────────┘

Task 11 ──── Task 12 (anthropic fixes) ─── Task 13, 14 (TUI) ─── Task 15 (cleanup) ─── Task 16, 17 ─── Task 18 (README)
```

Parallelizable: {1, 3, 4, 5, 6, 7} all have no shared dependencies after Task 0 (for non-Arcee paths).

## Success criteria (end-of-plan)

- `go test ./...` all green.
- `go build ./cmd/sam` produces a working binary.
- Manual TUI walkthrough: switch provider via `/provider`, `/model`, `/auth`; text + tool-use streams from OpenAI (`gpt-4o-mini`), Arcee (trinity), Anthropic (`claude-sonnet-4-5`), Minimax (`MiniMax-M2.7`).
- Multi-turn reasoning preserved across turns on Trinity (observable in `sam -p` output's debug log).
- No imports of `internal/llm/anthropic` or `internal/llm/minimax` remain.
- README lands accurate config + command reference.

## After completion

Run `caveroach:update-codebase-index` to capture the new package layout (openaicompat, registry, map-based config).
