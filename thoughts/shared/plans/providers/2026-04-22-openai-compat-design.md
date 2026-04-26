# OpenAI-Compatible Provider Support — Design

**Status:** Draft v3 — revised after spec review round 2
**Date:** 2026-04-22 (revised 2026-04-23)

### Revision history

- **v1** (2026-04-22): initial draft from brainstorm.
- **v2** (2026-04-22): addressed review blockers B1 (custom SSE reader), B2 (pre-impl probe §0), B3 (stream-interleave + loop.go citation), B4 ((state, carry) tuple); moved `reasoning_effort` off `llm.Request` (N1); added `SupportsSamplingParams`, `SupportsIncludeUsage`, `SupportsParallelToolCalls`, `SystemRoleFallback` caps; tool-error JSON wrapping (S2); new §7 for multi-turn reasoning preservation (driven by Trinity requirement found via web research); expanded test matrix; updated capability table with `ReasoningSource` and `EchoReasoning`.
- **v3** (2026-04-23): round-2 review amendments. Committed to owning the request-body struct (SDK reduced to types/constants, resolves extension-field problem in §7); scoped Anthropic extended-thinking round-trip OUT of §7 as YAGNI — thinking blocks on Anthropic history dropped pre-wire (avoids signed-block requirement); tightened think-tag parser to **peek-then-append** semantics with new boundary test; specified auth-header variants, non-200 body decoding, `Accept-Encoding: identity`, context-driven cancellation in §3.5; `parallel_tool_calls` now emitted only when forcing `false`; slug canonicalized to `trinity-large-thinking` pending §0 probe; preset merge semantics changed to full-replace when user defines a colliding key; tool-argument JSON-validity guard at stream close; dispatcher pinned to `internal/llm/registry.go`; §0 probe gate made explicit about which rollout steps it blocks.
**Scope:** Add support for (1) arbitrary OpenAI-compatible providers, (2) OpenAI GPT / o-series / GPT-5 models, (3) ArceeAI `trinity-large-thinking` (slug pending §0 probe). Deep integration with the OpenAI Chat Completions wire, including reasoning tokens, think-tag parsing, multi-turn reasoning preservation (§7), and per-model capability overrides.

## Goals

- One uniform code path for every OpenAI-compatible backend (OpenAI, Arcee, vLLM, Groq, Together, OpenRouter, DeepSeek, Ollama, LM Studio, …).
- First-class support for reasoning models (GPT-5, o-series) including `reasoning_effort` and hidden reasoning-token accounting.
- First-class support for inline `<think>` reasoning models (Arcee trinity-large-thinking variants served without server-side reasoning parser) with streaming tag extraction.
- Extensible provider configuration — adding a new backend is TOML-only, no code change.
- Preserve existing `llm.Provider` interface and agent loop; isolate all changes behind the interface.

## Non-Goals

- OpenAI Responses API. Chat Completions only. Can be added later as a per-entry `api = "responses"` switch.
- OpenAI file-upload, audio, image-input, or batch APIs. Text + tools only.
- Automatic cost tracking against live pricing tables.
- TUI-based editing of provider entries. Config file is the source of truth.

## Design Decisions (fixed during brainstorming)

| # | Decision | Choice |
|---|---|---|
| Q1 | Provider config shape | **B** — Extensible map `Providers map[string]ProviderEntry` with `wire` discriminator |
| Q2 | OpenAI API endpoint | **A** — Chat Completions only |
| Q3 | OpenAI client implementation | **Superseded v3 → C** — own request body + response parsing; stdlib only (see §3.5). v2's "SDK for types" plan was incompatible with §7's outbound `reasoning` field. Zero third-party deps for this wire. |
| Q4 | Reasoning / thinking surface | **A** — Unified: parse `delta.reasoning_content` + streaming `<think>` tag stripper + `reasoning_effort` passthrough |
| Q5-S | Secrets shape | **S2** — Each entry declares `api_key_env`; `secrets.toml` uses `[api_keys]` map |
| Q5-P | Bundled presets | **P2** — Ship built-in presets for minimax, anthropic, openai, arcee |
| Q6-R | Model → provider routing | **R3** — Hybrid: `/model openai/gpt-5` explicit form, bare `/model gpt-5` errors if ambiguous |
| Q6-E | Per-model `reasoning_effort` | **E2** — Per-model overrides via `[models."gpt-5"]` block |
| Q7 | Compat quirks (roles, param names) | **C** — Hybrid: hardcoded prefix table + per-entry and per-model overrides |
| Q8-M | Migration | **M3** — Breaking config change, one README note |
| Q8-U | `/provider`, `/auth` UX | **U1** — Extend existing commands to iterate the provider map |

---

## § 0 — Pre-implementation Verification (partial-blocker gate)

Two facts need live probe confirmation. The probe **blocks Arcee-specific implementation steps** (rollout steps 6 + the Trinity transcript in 4). All other work — config refactor, `ContentThinking`, OpenAI wire for GPT-4o / GPT-5, think-tag parser, `/provider`/`/model`/`/auth` UX — proceeds in parallel and is not gated.

Probes:

1. **Arcee Conductor wire shape.** Call `https://conductor.arcee.ai/v1/chat/completions` with the confirmed model slug, streaming. Record whether reasoning arrives as `delta.reasoning_content`, `delta.reasoning`, or inline `<think>…</think>` in `delta.content`. The public vLLM recipe uses `--reasoning-parser deepseek_r1`, which on non-streaming responses extracts `<think>` into a top-level `reasoning` field; streaming shape needs confirmation. Probe result drives the Arcee preset's `ReasoningSource` setting (§4) and `parse_think_tags` default.

2. **Exact model name.** Hugging Face card is `arcee-ai/Trinity-Large-Thinking`. Arcee docs, OpenRouter, and Conductor deployments may use different slugs. Working assumption in this document: **`trinity-large-thinking`** (lowercase, hyphenated, `large` before `thinking` matching the HF card) — used verbatim in §2 preset and §4 cap prefix. Probe confirms or corrects.

Probe process: ~5 minutes with `curl` once an API key is available. Results land as a short followup commit to this doc. Rollout steps 1–5, 7–10 proceed without waiting.

---

## § 1 — Architecture & Package Layout

**New package:** `internal/llm/openaicompat/`

| File | Purpose |
|---|---|
| `client.go` | HTTP client + streaming coordinator. Holds `apiKey`, `baseURL`, `caps`, `effortResolver func(model) string`, `authHeader` strategy. |
| `sse.go` | Minimal SSE line reader (~60 lines). Decodes `data: {...}` frames, stops on `[DONE]`. |
| `wire.go` | **Our own** request-body struct + `delta` / `streamChunk` response structs. SDK types are NOT used in the request path — see §3.5. |
| `stream.go` | Converts decoded chunks into `llm.StreamEvent`s. Owns tool-call accumulator. |
| `translate.go` | Pure: `toWireMessages`, `toWireTools`, `mapFinishReason`. |
| `thinktag.go` | Streaming `<think>…</think>` state-machine parser (state + carry tuple). |
| `caps.go` | `Capabilities` struct + `defaultCaps(model)` prefix table + merge helpers. |
| `provider.go` | Implements `llm.Provider`. `New(Options) (*Provider, error)`. |

**New dispatcher file:** `internal/llm/registry.go` (pinned — no longer "either here or there"). Owns `Build(entry config.ProviderEntry, model string) (llm.Provider, error)` switching on `entry.Wire`. Testable without `cmd/sam/main.go` wiring.

**Existing packages touched:**

- `internal/llm/provider.go` — unchanged (interface stable).
- `internal/llm/types.go` — add a new `ContentThinking` content type to `ContentBlock` (see §7). No change to `llm.Request` — `reasoning_effort` is resolved inside the openai wire via a closure, keeping OpenAI-isms out of the cross-provider shape.
- `internal/llm/anthropic/`, `internal/llm/minimax/` — **removed**. Both were thin wrappers over `anthropiccompat`. They become preset entries in the provider map using `wire = "anthropic"`. `anthropiccompat` gains a `NewProvider` symmetric to `openaicompat.NewProvider`.
- `internal/config/config.go` — replace hardcoded provider struct with map; add preset merge; extend `ModelConfig`.
- `internal/config/secrets.go` — replace hardcoded fields with `map[string]string`; env export iterates provider map.
- `cmd/sam/main.go` — `buildProvider` delegates to `llm.Build` from `internal/llm/registry.go`.
- `internal/tui/` — `/provider`, `/model`, `/auth` commands updated; settings tab gains read-only provider list.

**Dispatcher** (`internal/llm/registry.go`):

```go
func Build(entry config.ProviderEntry, model string, resolver func(string) string) (llm.Provider, error) {
    switch entry.Wire {
    case "anthropic":
        return anthropiccompat.NewProvider(entry, model)
    case "openai":
        return openaicompat.NewProvider(entry, model, resolver)
    default:
        return nil, fmt.Errorf("unknown wire: %s", entry.Wire)
    }
}
```

`resolver` supplies per-model `reasoning_effort` strings; only consumed by the openai wire.

**File count:** ~6 new files, 2 packages deleted, 4 files modified.

---

## § 2 — Config Schema

### `config.toml`

```toml
provider = "minimax"
model    = "MiniMax-M2.7"            # bare or "provider/model"
max_tokens     = 4096
max_iterations = 25

[providers.minimax]
wire          = "anthropic"
base_url      = "https://api.minimax.io/anthropic"
api_key_env   = "MINIMAX_API_KEY"
default_model = "MiniMax-M2.7"

[providers.anthropic]
wire          = "anthropic"
base_url      = ""                   # SDK default
api_key_env   = "ANTHROPIC_API_KEY"
default_model = "claude-sonnet-4-5"

[providers.openai]
wire          = "openai"
base_url      = ""                   # SDK default
api_key_env   = "OPENAI_API_KEY"
default_model = "gpt-5"

[providers.arcee]
wire             = "openai"
base_url         = "https://conductor.arcee.ai/v1"
api_key_env      = "ARCEE_API_KEY"
default_model    = "trinity-large-thinking"          # §0 probe confirms exact slug
parse_think_tags = false                             # §0 probe sets this; default assumes Conductor extracts to reasoning_content/reasoning

# User-defined (not a preset)
[providers.groq]
wire          = "openai"
base_url      = "https://api.groq.com/openai/v1"
api_key_env   = "GROQ_API_KEY"
default_model = "llama-3.3-70b-versatile"

# Optional per-entry capability overrides
[providers.openai.caps]
system_role          = "developer"
max_tokens_field     = "max_completion_tokens"
supports_temperature = false
supports_reasoning_effort = true

# Per-model overrides (extends existing [models.*])
[models."gpt-5"]
context_window    = 400000
reasoning_effort  = "medium"

[models."trinity-large-thinking"]
context_window = 512000
```

### `ProviderEntry` Go struct

```go
type ProviderEntry struct {
    Name            string         // map key, filled after unmarshal
    Wire            string         `toml:"wire"`              // "anthropic" | "openai"
    BaseURL         string         `toml:"base_url"`
    APIKeyEnv       string         `toml:"api_key_env"`
    DefaultModel    string         `toml:"default_model"`
    Models          []string       `toml:"models"`            // optional, for R3 ambiguity resolution
    ModelPrefixes   []string       `toml:"model_prefixes"`    // optional
    ParseThinkTags  bool           `toml:"parse_think_tags"`
    Caps            CapsOverride   `toml:"caps"`              // optional overrides
}

type CapsOverride struct {
    SystemRole                *string `toml:"system_role"`
    SystemRoleFallback        *string `toml:"system_role_fallback"`
    MaxTokensField            *string `toml:"max_tokens_field"`
    SupportsSamplingParams    *bool   `toml:"supports_sampling_params"`
    SupportsReasoningEffort   *bool   `toml:"supports_reasoning_effort"`
    SupportsIncludeUsage      *bool   `toml:"supports_include_usage"`
    SupportsParallelToolCalls *bool   `toml:"supports_parallel_tool_calls"`
    EchoReasoning             *bool   `toml:"echo_reasoning"`
    ReasoningSource           *string `toml:"reasoning_source"` // "none" | "inline_think" | "reasoning_content" | "reasoning" | "both"
    AuthHeader                *string `toml:"auth_header"`      // "bearer" | "api-key" | "none"
}
```

### Presets (code-defined, `internal/config/presets.go`)

Hardcoded `map[string]ProviderEntry` for `minimax`, `anthropic`, `openai`, `arcee`.

**Merge semantics** (clarified in v3): if a user's `config.toml` declares a provider key that already exists as a preset, the user entry **fully replaces** the preset entry — no field-by-field merge. Rationale: field-merge surprises users who repurpose the `openai` key for LM Studio (expecting `default_model` to reset but it survives from the preset instead). Full-replace keeps the mental model predictable: "if I write an entry in TOML, TOML is the source of truth for that entry."

New keys (e.g. `groq`, `local-vllm`) append. Presets cannot be deleted, only replaced.

### `secrets.toml`

```toml
[api_keys]
minimax   = "..."
anthropic = "..."
openai    = "sk-..."
arcee     = "..."
```

### `ExtendedModelConfig`

```go
type ModelConfig struct {
    ContextWindow    int    `toml:"context_window"`
    ReasoningEffort  string `toml:"reasoning_effort"`   // "low" | "medium" | "high" | "minimal"
}
```

### `LoadSecrets` / `ApplyEnv`

```go
type Secrets struct {
    APIKeys map[string]string `toml:"api_keys"`
}

func (s Secrets) ApplyEnv(cfg *Config) {
    for name, entry := range cfg.Providers {
        if key, ok := s.APIKeys[name]; ok && key != "" && os.Getenv(entry.APIKeyEnv) == "" {
            os.Setenv(entry.APIKeyEnv, key)
        }
    }
}
```

### Validation at Load

- Every `cfg.Providers[name].Wire` must be `"anthropic"` or `"openai"`.
- `cfg.Provider` must exist in the merged map.
- `cfg.Model`, if `<provider>/<model>` form, the provider part must exist.

---

## § 3 — OpenAI Wire Adapter

### § 3.5 — We own the HTTP path (resolves B1 + review-2 §1 + §7 2a)

**Rationale.** `sashabaranov/go-openai` does not expose `delta.reasoning_content`, `delta.reasoning`, `delta.refusal`, or `completion_tokens_details.reasoning_tokens` as typed fields, its `ChatCompletionStream.Recv()` hides the raw chunk JSON, and crucially its `ChatCompletionMessage` cannot carry an outbound `reasoning` field (needed by §7 for Trinity multi-turn). Extending the SDK's types inline (struct embedding + extra field) does not round-trip through its `ChatCompletionRequest` marshaling because the SDK owns the `Messages []openai.ChatCompletionMessage` slice type.

**Commitment.** Define our own request and response structs in `wire.go`. The SDK dependency collapses to role constants (`openai.ChatMessageRoleUser` / `Assistant` / `System` / `Tool` / `Developer`) if we want them — or we inline those constants and drop the SDK dependency entirely. Lean: drop the SDK. Zero transitive deps for this wire.

```go
// wire.go (sketch)
type chatRequest struct {
    Model               string          `json:"model"`
    Messages            []chatMessage   `json:"messages"`
    Stream              bool            `json:"stream"`
    StreamOptions       *streamOptions  `json:"stream_options,omitempty"`
    Tools               []chatTool      `json:"tools,omitempty"`
    ToolChoice          any             `json:"tool_choice,omitempty"`
    ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"` // emit only when forcing false
    MaxTokens           *int            `json:"max_tokens,omitempty"`
    MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
    Temperature         *float64        `json:"temperature,omitempty"`
    TopP                *float64        `json:"top_p,omitempty"`
    PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
    FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
    ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
}

type chatMessage struct {
    Role       string          `json:"role"`                 // "system"|"developer"|"user"|"assistant"|"tool"
    Content    *string         `json:"content,omitempty"`    // pointer → omit entirely when nil
    Reasoning  string          `json:"reasoning,omitempty"`  // §7 multi-turn preservation
    ToolCalls  []chatToolCall  `json:"tool_calls,omitempty"` // assistant only
    ToolCallID string          `json:"tool_call_id,omitempty"` // tool role only
}

type chatToolCall struct {
    ID       string `json:"id"`
    Type     string `json:"type"` // always "function"
    Function struct {
        Name      string `json:"name"`
        Arguments string `json:"arguments"`
    } `json:"function"`
}

type chatTool struct {
    Type     string `json:"type"` // "function"
    Function struct {
        Name        string         `json:"name"`
        Description string         `json:"description"`
        Parameters  map[string]any `json:"parameters"`
    } `json:"function"`
}
```

Response / stream types mirror the request — `delta` carries `content *string`, `refusal *string`, `reasoning_content string`, `reasoning string`, `tool_calls []streamToolCallDelta`. Unknown fields are tolerated by `encoding/json`'s default behavior.

**HTTP path (`client.go`):**

1. **Request.** `http.NewRequestWithContext(ctx, "POST", baseURL + "/chat/completions", body)`. Headers:
   - `Content-Type: application/json`
   - Auth: `caps.AuthHeader` drives it — `"bearer"` → `Authorization: Bearer <key>` (default for openai, arcee, deepseek, groq, openrouter, most compat); `"api-key"` → `api-key: <key>` (Azure OpenAI); `"none"` → no header (local Ollama). Per-entry override via `CapsOverride.AuthHeader`. Presets: all four use `"bearer"`.
   - `Accept: text/event-stream`
   - `Accept-Encoding: identity` — explicit, to avoid proxy gzip breaking SSE line buffering.
2. **Transport.** Use `http.DefaultTransport` (honors `HTTPS_PROXY` / `HTTP_PROXY` automatically). No `Client.Timeout` — streams are long-lived; cancellation is driven by `ctx` passed through `Stream(ctx, req)` into both `http.NewRequestWithContext` and the SSE scanner loop. Closing `ctx` aborts both.
3. **Retry.** None on the streaming path — mid-stream retry is non-idempotent and risks duplicated tokens/tool calls. Document explicitly.
4. **Non-200 handling.** Read the response body (cap at ~64KB) and attempt to decode `{"error":{"message":"...","type":"...","code":"..."}}`. Emit a single `EventError` with a wrapped error: `fmt.Errorf("openai %d %s: %s", status, errType, message)`. Fall back to raw body snippet if JSON decode fails. Status 429 → include `Retry-After` header value in the message. Status-code classification is flat (no retry logic); users read the error.
5. **SSE loop.** `bufio.Scanner` reads lines. Blank lines split frames; `: ...` lines are comments (heartbeats) and ignored. `data: [DONE]` ends the stream cleanly. `data: {json}` passes to the delta decoder. Malformed frames → emit `EventError` and stop.
6. **Cancellation.** On `ctx.Done()` mid-stream, closing the response body aborts the scanner; we emit `EventError` with `ctx.Err()` and return.

**Dependencies.** Zero third-party imports in `openaicompat/` — `encoding/json`, `net/http`, `bufio`, stdlib only. This is a net simplification.

### Message translation (`translate.go`)

Input: `[]llm.Message`. Output: `[]chatMessage` (from `wire.go`).

Rules:

1. **User message** with `[]ContentBlock`:
   - Concatenate `ContentText` blocks into one `{role:"user", content:...}` message (only emit if any text exists).
   - For each `ContentToolResult` block, emit a separate `{role:"tool", tool_call_id:<block.ToolUseID>, content:<tool_result_payload>}` message **in order**.
   - **Tool error encoding:** `block.IsError == true` → wrap output as JSON: `{"error": true, "output": "<text>"}`. Success → raw text. This is more reliable than a `[error] ` prefix (reviewer S2); models parse the JSON shape predictably.
2. **Assistant message**:
   - Text blocks → `content` (string).
   - `ContentThinking` blocks (§7) → `reasoning` field on the message for Trinity/DeepSeek; dropped when `caps.EchoReasoning == false` (OpenAI GPT-5 rejects it). Always round-trip when `EchoReasoning == true`.
   - `ContentToolUse` blocks → `tool_calls: [{id, type:"function", function:{name, arguments: string(block.Input)}}]`.
   - Mixed text + tool_use: single message carrying both fields. **Do not send `content: ""`** — omit the field entirely (some compat servers reject empty string).
   - Existing agent loop at `loop.go:95-131` already accumulates both text blocks and tool_use blocks into the same assistant `msg.Content`, so round-tripping text+tool_use is already correct on the history side; this section only formalizes the wire projection.
3. **System prompt** (`req.System`): prepend as first message, role = `caps.SystemRole`. If `caps.SystemRoleFallback` is set and the server rejects the primary role, retry once with the fallback (rare; debug-log before shipping).

**Tool schema translation:**

```go
for _, td := range req.Tools {
    tools = append(tools, chatTool{
        Type: "function",
        Function: struct {
            Name        string         `json:"name"`
            Description string         `json:"description"`
            Parameters  map[string]any `json:"parameters"`
        }{
            Name:        td.Name,
            Description: td.Description,
            Parameters:  td.Schema,          // JSON Schema map verbatim
        },
    })
}
```

Fixes existing `anthropiccompat/client.go` bug around line 94 where `required` is populated with every property name. Both wires should read `required` from the schema as-is and not synthesize it. (Side fix to `anthropiccompat` included in the rollout.)

### Request params

| Wire field | Source |
|---|---|
| `model` | `req.Model` |
| `messages` | translation above |
| `tools` | translation above; **field + `tool_choice` both omitted when empty** (Groq and some vLLM builds 400 on empty tools) |
| `stream` | always `true` |
| `stream_options.include_usage` | only when `caps.SupportsIncludeUsage == true`; default true, but settable false for old Ollama / vLLM that 400 on it |
| `max_tokens` | if `caps.MaxTokensField == "max_tokens"` |
| `max_completion_tokens` | if `caps.MaxTokensField == "max_completion_tokens"` |
| `temperature`, `top_p`, `presence_penalty`, `frequency_penalty` | omitted when `caps.SupportsSamplingParams == false` (reviewer N3 — broader than temperature) |
| `reasoning_effort` | only when `caps.SupportsReasoningEffort == true`; value resolved by `effortResolver(model)` closure at request time (per-model TOML override > entry default > empty) |
| `parallel_tool_calls` | **omit unless forcing false.** Server default is already `true`. Emit `false` only when `caps.SupportsParallelToolCalls == false` (for servers that choke on the field entirely, keep it unset regardless). Saves a token on the happy path and avoids 400s on servers that don't know the field. |

### Stream translation (`stream.go`)

Consume decoded `streamChunk`s from `sse.go` in order. Per chunk **deltas are processed in this fixed order: content → reasoning → tool_calls → refusal → finish_reason**:

- **First chunk** → emit `EventMessageStart`.
- `delta.content`:
  - When `entry.ParseThinkTags == true`, feed through `thinktag.Parser`. Emit `EventTextDelta` for text part, `EventThinkingDelta` for thinking part (each only if non-empty).
  - Else emit `EventTextDelta` directly.
- `delta.reasoning_content` OR `delta.reasoning` (whichever is non-empty; both mapped identically to `EventThinkingDelta`). If both populated in the same chunk (pathological), concatenate `reasoning_content` first then `reasoning`.
- `delta.refusal` (GPT-4o/GPT-5 safety refusals): if non-empty, emit `EventError` wrapping `fmt.Errorf("refusal: %s", …)` and drain the rest of the stream. Not surfaced as text (reviewer N3).
- `delta.tool_calls[]`: accumulate by `index`:
  - First appearance of a given index (populated `id` and/or `function.name`) → emit `EventToolUseStart{ToolUseID, ToolName}`. Capture `id` on first sight — OpenAI sends it only once per tool; subsequent chunks for the same index have empty `id`.
  - Subsequent chunks for that index (any `function.arguments` string, possibly empty) → emit `EventToolUseDelta{ToolUseID, PartialJSON}`.
  - **No per-call stop event is emitted by OpenAI.** Do NOT flush stops on index change — parallel tool calls interleave across indices. Defer all stops until `finish_reason` arrives.
- `finish_reason` (arrives on the last chunk, may co-occur with `delta.content` or `delta.tool_calls` in the same chunk — deltas handled first, then finalize):
  - If tool calls tracked, emit `EventToolUseStop` for every tracked index in registration order. **JSON-validity guard:** before emitting stop for each index, validate the accumulated `function.arguments` buffer with `json.Valid`. Invalid → emit `EventError` wrapping `fmt.Errorf("tool %q: invalid JSON arguments (finish_reason=%s): %q", name, reason, buf)` and abort the turn. Never silently emit a well-formed empty `{}` to paper over the failure — the agent's downstream tool execution would run with wrong inputs. (Happens on compat servers like Groq when `finish_reason=length` truncates mid-args; surfacing the error lets the user raise `max_tokens` rather than run the tool on garbage.)
  - Map reason: `tool_calls` → `"tool_use"`, `function_call` (legacy path) → `"tool_use"`, `stop` → `"end_turn"`, `length` → `"max_tokens"`, `content_filter` → `"content_filter"`, missing/null/empty on clean close → `"end_turn"` (debug-log), any other → passthrough.
- `usage` (may arrive with `finish_reason` or in a trailing chunk):
  - Emit `EventMessageStop{StopReason, InputTokens: prompt_tokens, OutputTokens: completion_tokens}`. `completion_tokens_details.reasoning_tokens` is already counted inside `completion_tokens` by OpenAI accounting; no double-counting needed. `CacheReadInput`/`CacheCreationInput` stay 0.
- Transport error / JSON decode error / abrupt close → emit `EventError`.

### Parallel tool calls

Passthrough. Agent loop at `loop.go:44-57` collects all `pending` ToolCalls across a stream and emits one user message with N tool_result blocks. Our translator splits those back into N tool-role messages on the next turn. Index interleaving in the stream is handled by the accumulator above; no extra logic needed.

### Stream-interleaving note (resolves review B3)

OpenAI may interleave `delta.content` with `delta.tool_calls[]` chunks (usually text first, then tool calls; no ordering guarantee). Our translator emits events in arrival order, so a mixed assistant message (some text, then a tool_use) is preserved on the downstream agent side. The existing agent loop at `loop.go:95-131` already stores both text and tool_use blocks into one assistant `msg.Content` — no change needed on that side.

### Content-with-null quirk

Some compat servers send `delta.content: null` to indicate "no content this chunk" rather than omitting the field. Our `streamChunk` decodes `content` as `*string`; nil is ignored, empty string is treated as no-op text delta (skipped).

---

## § 4 — Reasoning & Think-Tag Handling

### Capabilities struct + table (`caps.go`)

```go
type Capabilities struct {
    SystemRole               string // "system" | "developer"
    SystemRoleFallback       string // optional; retry once with this if primary 400s
    MaxTokensField           string // "max_tokens" | "max_completion_tokens"
    SupportsSamplingParams   bool   // temperature, top_p, presence/frequency_penalty
    SupportsReasoningEffort  bool
    SupportsIncludeUsage     bool   // default true
    SupportsParallelToolCalls bool  // default true
    EchoReasoning            bool   // true → round-trip reasoning field on assistant history
    ReasoningSource          string // "none" | "inline_think" | "reasoning_content" | "both"
}
```

Matched by longest model-name prefix. Applied *before* entry/model overrides.

| Prefix | SystemRole | MaxTokensField | Sampling | ReasoningEffort | ReasoningSource | EchoReasoning |
|---|---|---|---|---|---|---|
| `gpt-5`, `o1`, `o3`, `o4` | `developer` | `max_completion_tokens` | ❌ | ✅ | `none` (hidden) | ❌ |
| `gpt-4`, `gpt-3.5`, `gpt-4o` | `system` | `max_tokens` | ✅ | ❌ | `none` | ❌ |
| `deepseek-r` | `system` | `max_tokens` | ✅ | ❌ | `reasoning_content` | ✅ |
| `trinity-large-thinking` | `system` | `max_tokens` | ✅ | ❌ | pending §0 probe (`reasoning` \| `reasoning_content` \| `inline_think`) | ✅ |
| _default_ | `system` | `max_tokens` | ✅ | ❌ | `none` | ❌ |

Defaults `SupportsIncludeUsage: true`, `SupportsParallelToolCalls: true` everywhere.

**Resolution order:** per-model override → per-entry override → prefix default.

### `reasoning_effort` plumbing (resolves review N1)

- **No change to `llm.Request`.** Keeping OpenAI-isms out of the cross-provider interface.
- `openaicompat.Provider` holds an `effortResolver func(model string) string` injected at construction time.
- In `cmd/sam/main.go`, `buildProvider` wires: `effortResolver = func(m string) string { return cfg.Models[m].ReasoningEffort }`.
- Request building calls `effortResolver(req.Model)` and sets the wire field only when `caps.SupportsReasoningEffort == true` AND the string is non-empty.
- Anthropic wire never sees the field. Future `/reasoning <level>` runtime override (Follow-ups) would flip to a request-scoped mechanism at that time.

### Think-tag streaming parser (`thinktag.go`) (resolves review B4)

State machine with **persistent `(state, carry)` across chunks** — the parser is stateful between `Feed(chunk)` calls. Parser instance is created per stream; callers pass every text delta through it in order and call `Flush()` at stream end.

```go
type Parser struct {
    state State        // OUTSIDE | MAYBE_OPEN | INSIDE | MAYBE_CLOSE
    carry []byte       // partial tag bytes not yet classified
}
func (p *Parser) Feed(chunk string) (text, thinking string)
func (p *Parser) Flush() (text, thinking string)
```

Transitions — **peek-then-append semantics** (resolves review-2 §3 ambiguity):

- `OUTSIDE`: emit bytes to text until `<` → transition to `MAYBE_OPEN`, carry = `<`.
- `MAYBE_OPEN`: for each incoming byte `b`:
  - **Peek:** would `carry + b` still be a strict prefix of `<think>`? (i.e. equal to `<`, `<t`, `<th`, `<thi`, `<thin`, `<think`).
    - Yes → append `b` to carry. Continue.
  - Does `carry + b` equal the full string `<think>`?
    - Yes → append `b`, then transition to `INSIDE`, clear carry.
  - Otherwise (divergence): **do not append `b`**. Flush current carry as text. Transition to `OUTSIDE`. **Reprocess `b` through the state machine from the top** (so `b == '<'` re-enters `MAYBE_OPEN` with carry = `<` — covers `<<think>` case).
- `INSIDE`: emit bytes to thinking until `<` → transition to `MAYBE_CLOSE`, carry = `<`.
- `MAYBE_CLOSE`: mirror of `MAYBE_OPEN` against `</think>`. Strict-prefix peek keeps buffering. Full match appends `b`, transitions to `OUTSIDE`, clears carry. Divergence flushes carry as thinking, transitions to `INSIDE`, reprocesses `b` (so `b == '<'` re-enters `MAYBE_CLOSE` with carry = `<`).

`carry` persists verbatim across `Feed` boundaries — the next call continues the same scan. Each `Feed` returns what was fully classified during this call; anything still in carry is held for the next call (or for `Flush`).

`Flush()` behavior:
- `OUTSIDE` with empty carry → no-op.
- `MAYBE_OPEN` with non-empty carry → flush carry as text (never was a tag).
- `INSIDE` with empty carry → no-op.
- `MAYBE_CLOSE` with non-empty carry → flush carry as thinking.
- Terminal `INSIDE` with no close tag (unclosed `<think>`): already streamed as thinking during `Feed`; `Flush` just returns empty.

**Tests** (every case run with chunk splits at every byte boundary 1..N):

| Case | Input (as one string) | Expected text | Expected thinking |
|---|---|---|---|
| plain | `hello world` | `hello world` | `` |
| one tag | `hi <think>ok</think>bye` | `hi bye` | `ok` |
| adjacent tags | `<think>a</think><think>b</think>` | `` | `ab` |
| unclosed | `pre<think>tail` | `pre` | `tail` |
| false positive | `drink <thirsty> water` | `drink <thirsty> water` | `` |
| `<<think>` | `<<think>x</think>` | `<` | `x` |
| `<` + `<think>x</think>` split | 2-chunk: `<`, then `<think>x</think>` | `<` | `x` |
| split at `<t` | fed in 2-byte chunks | (same as plain-tag golden) | |
| nested (literal) | `<think>a<think>b</think>c` | `c` | `a<think>b` |

Parser lives inside `stream.go` per active stream; it only runs when `entry.ParseThinkTags == true`.

---

## § 7 — Multi-turn Reasoning Preservation (new)

**Motivation.** Trinity-Large-Thinking and DeepSeek-R1 require the model's prior reasoning to be echoed back into the next turn's assistant history, or multi-step agentic performance degrades significantly (per Arcee/vLLM docs). Our current agent loop emits `ThinkingDelta` events to the UI but **does not store the thinking text** on the assistant `llm.Message.Content` — at `loop.go` around line 90 the event is only forwarded, never appended to `msg.Content`. On the next turn the history sent to the provider has no trace of the prior reasoning. This needs fixing for the OpenAI-compat wire.

### Scope (narrowed in v3)

- **In scope:** OpenAI-compat wire only. Round-tripping is needed for Trinity, DeepSeek, and future reasoning-heavy compat models.
- **Out of scope:** Anthropic extended-thinking round-trip. Claude's extended thinking requires server-issued **signed** thinking blocks (containing `signature` and, for redacted thinking, `data` fields) that must be passed back verbatim — cryptographically bound to the request. Capturing just the text (as our `ContentBlock` currently does) and replaying it would be rejected with a 400 signature mismatch. Properly supporting this needs a new signed-block storage on `ContentBlock` and a deeper change to `anthropiccompat`. Deferred to a follow-up. For now, `anthropiccompat` drops `ContentThinking` blocks pre-wire; current Anthropic integration stays exactly as it is today, where extended thinking is single-turn visible-only. Users do not lose anything they already have.

### Changes

**1. New content type in `internal/llm/types.go`:**

```go
const ContentThinking ContentType = "thinking"
```

`ContentBlock{Type: ContentThinking, Text: "..."}` represents a model's reasoning trace. No new fields — `Text` carries the reasoning content. (A future Anthropic extended-thinking round-trip effort will add `Signature` and `Data`; not now.)

**2. Agent loop (`internal/agent/loop.go`):**

On `EventThinkingDelta`, accumulate into a running thinking block on `msg.Content`, mirroring the text-block accumulator logic already present for text deltas:

```go
case llm.EventThinkingDelta:
    emitToChan(out, ThinkingDelta{Text: ev.Text}, reqCtx)
    if n := len(msg.Content); n > 0 && msg.Content[n-1].Type == llm.ContentThinking {
        msg.Content[n-1].Text += ev.Text
    } else {
        msg.Content = append(msg.Content, llm.ContentBlock{Type: llm.ContentThinking, Text: ev.Text})
    }
```

Ordering: text, thinking, and tool_use blocks all accumulate into the same assistant message. Relative order is preserved.

**3. Anthropic wire (`anthropiccompat/client.go`):**

On outbound message translation, `ContentThinking` blocks are **dropped silently** before the wire call. No attempt to round-trip into Anthropic's signed thinking block shape (see Scope above). This is a no-op for current users (they have no `ContentThinking` blocks today).

**4. OpenAI wire (`openaicompat/translate.go`):**

On assistant message translation:
- If `caps.EchoReasoning == true`: concatenate all `ContentThinking` block texts in order and set `chatMessage.Reasoning` on the assistant message. Our `chatMessage` type (§3.5) carries this field natively. vLLM recent versions accept both `reasoning` and `reasoning_content` as input; we emit `reasoning` (matching OpenRouter and recent vLLM conventions per web research).
- If `caps.EchoReasoning == false` (OpenAI GPT-5/o-series, generic non-reasoning backends): drop `ContentThinking` blocks silently. OpenAI reasoning tokens are server-side and should not be echoed.
- **Cross-model history:** when the user switches model mid-conversation, the translator still iterates all prior assistant messages uniformly. For a Trinity→GPT-5 switch, prior `ContentThinking` blocks get dropped by the GPT-5 `EchoReasoning=false` rule — correct. For the reverse (GPT-5→Trinity), GPT-5's hidden reasoning was never captured as `ContentThinking` blocks, so Trinity sees no reasoning for pre-switch turns — acceptable; reasoning kicks in from the first Trinity-authored turn onward.

**5. Stream translation (already covered in §3):**

`delta.reasoning_content` / `delta.reasoning` → `EventThinkingDelta` → agent loop accumulator (#2) → `ContentThinking` block on assistant history → echoed back on next turn via #4. Full round-trip on the OpenAI-compat wire.

**6. Context-window accounting.**

Thinking tokens are part of the history now. Per-model `context_window` defaults in `internal/config/config.go` (`ModelContextWindow`) extended with prefix rules:
- `trinity-large-thinking`, `trinity-` → 512000
- `deepseek-r`, `deepseek-v3` → 131072
- `gpt-5`, `o1`, `o3`, `o4` → 400000
- `gpt-4o`, `gpt-4.1` → 128000

Existing `[models.<name>]` override still wins.

**7. Fake provider + UI.**

`internal/llm/fake/` — if tests emit thinking events, they must also round-trip `ContentThinking` blocks. TUI debug overlay already renders `ThinkingDelta` events — no change there.

### Edge cases

- Empty thinking block: drop pre-wire (`len(strings.TrimSpace(Text)) == 0`).
- Assistant message with thinking + tool_use but no text: emit `{role:"assistant", reasoning:"…", tool_calls:[...]}` with `content` **omitted entirely** (our `chatMessage.Content` is `*string`, nil → omitted by `omitempty`). A canned SSE test fixture must cover this shape.
- GPT-5 reasoning tokens are server-side → `EventThinkingDelta` never fires for them → no `ContentThinking` blocks are created → `EchoReasoning=false` is belt-and-suspenders rather than load-bearing.
- Cross-model switches: see #4 above. No user-facing surprise; document in README.

---

## § 5 — UX: Slash Commands & Keys

### `/provider <name>`

Resolves `<name>` against merged map. Unknown → error listing all available. Triggers `ProviderFactory` rebuild (existing hook in `tui.Options`).

### `/model <spec>` (R3)

- `<provider>/<model>` form: switch provider if different, set model.
- Bare `<model>`: scan entries in declaration order, collect matches via:
  1. Exact match with `entry.DefaultModel`.
  2. Membership in `entry.Models`.
  3. Match against `entry.ModelPrefixes` (or the hardcoded prefix table from §4).
- Zero → `unknown model; try 'provider/model'`.
- One → switch silently.
- Two+ → `ambiguous; matches: [openai, groq]; use 'openai/gpt-5'`.

### `/auth`

- No arg → render list in TUI with status column (`✓ key set` / `✗ missing`), one row per entry in the map.
- `<name>` → prompt for API key, save to `secrets.toml` under `[api_keys].<name>`, call `ApplyEnv`, rebuild active provider if `<name>` matches current.

### `/settings` (existing tabbed modal, commit `e3335b3`)

Add a **Providers** tab: read-only table (name, wire, base_url, key status, default_model). Editing happens via config file — YAGNI.

### Status bar

Render active combo as `openai/gpt-5` always. Unambiguous.

### Env vars

`SAM_PROVIDER`, `SAM_MODEL`, and every entry's `api_key_env` continue to work. `SAM_MODEL` accepts bare or namespaced form.

---

## § 6 — Testing, Rollout, Risks

### Tests (new)

- `openaicompat/translate_test.go` — table-driven:
  - User-text only.
  - Assistant text-only, assistant text+tool_use, assistant tool_use-only.
  - User with N `ContentToolResult` blocks → N `role:"tool"` messages in order; error results JSON-wrapped as `{"error":true,"output":"..."}`.
  - Assistant with `ContentThinking` block → `reasoning` field when `EchoReasoning=true`, dropped when false.
  - Tool schema passthrough including a case with explicit `required` and one without.
  - `tools` + `tool_choice` both omitted when `req.Tools` empty.
  - Empty assistant content + tool_calls → field omitted, not `""`.
- `openaicompat/thinktag_test.go` — table-driven over every case in §4 table, each fed in 1, 2, 3, …, N byte chunks. Output must be byte-identical regardless of split. Parser instance persists across `Feed` calls.
- `openaicompat/sse_test.go` — SSE framing: multi-line `data:` fields, `: comment` lines, `[DONE]` sentinel, malformed JSON in a frame, abrupt EOF.
- `openaicompat/stream_test.go` — canned transcripts → asserted `StreamEvent` sequence. One transcript per:
  - plain GPT-4o text response
  - GPT-5 (reasoning tokens hidden, only in `usage.completion_tokens_details.reasoning_tokens`)
  - DeepSeek-R1 with `delta.reasoning_content`
  - Trinity-Large-Thinking with `delta.reasoning` (pending §0 probe; fallback transcript with inline `<think>` if probe finds that shape)
  - parallel tool calls interleaved by index
  - refusal (`delta.refusal` non-empty)
  - `finish_reason` co-occurring with `delta.content`
  - missing `finish_reason` on clean close
  - tool-call `id` only on first chunk for its index
  - `finish_reason=tool_calls` with invalid JSON in accumulated arguments → `EventError` (not silent empty-object)
  - assistant message with `ContentThinking` + `ContentToolUse` and no text → serialized with `reasoning` + `tool_calls` and `content` field omitted (not `""`)
- `openaicompat/caps_test.go` — prefix lookup; override merge order (model > entry > prefix); `SupportsSamplingParams=false` drops all four sampling fields; `EchoReasoning` behavior.
- `config/config_test.go` — extend: map-shaped TOML; preset + user entry merge; missing wire errors; per-model `reasoning_effort` and `context_window` resolution; legacy hardcoded fields produce a clean error (M3 breaking change message).
- `config/secrets_test.go` — `[api_keys]` map loads; `ApplyEnv` respects existing env; per-entry `api_key_env` honored.
- `agent/agent_test.go` — add fake-provider case: thinking deltas round-trip into `ContentThinking` blocks on assistant history, and subsequent request's Messages contain the thinking content.
- `anthropiccompat/translate_test.go` — new: `ContentThinking` blocks round-trip for extended-thinking models; dropped for non-extended-thinking models.

### Smoke tests (opt-in, gated by env)

- `TestProviderLiveOpenAI` — skip unless `OPENAI_API_KEY` set; one completion against `gpt-4o-mini` (cheap). Separate test for `gpt-5-mini` if key tier permits; verifies `reasoning_effort=minimal` passthrough.
- `TestProviderLiveArcee` — skip unless `ARCEE_API_KEY` set; one completion against trinity; verifies the §0-probe'd reasoning field shape round-trips.
- `TestMultiTurnReasoningPreservation` — optional, live — run a 2-turn convo against trinity with a tool call, assert the second turn's request body includes the first turn's reasoning.
- Mirrors `internal/llm/anthropic/integration_test.go` style.

### Rollout order

0. **Pre-implementation probe** (§0) — confirm Arcee wire shape + exact model slug.
1. Config schema refactor + presets + secrets map + per-model `context_window` defaults. Anthropic wire unchanged and still green.
2. `llm.ContentThinking` addition; agent loop accumulator change; anthropic-wire round-trip of thinking blocks; tests green.
3. `openaicompat`: `translate.go` + `thinktag.go` + `caps.go` + their tests.
4. `openaicompat`: `sse.go` + `chunk.go` + `stream.go` against canned SSE transcripts.
5. `openaicompat`: `client.go` + `provider.go`; wire into `buildProvider`. Live call against `openai` preset (`gpt-4o-mini` first — cheap, non-reasoning; then `gpt-5-mini`).
6. Arcee preset (with §0-confirmed fields). Live call against trinity. Multi-turn reasoning round-trip verified.
7. `reasoning_effort` plumbing via `effortResolver` + per-model TOML.
8. `/provider`, `/model`, `/auth` updates + settings tab.
9. Delete `internal/llm/anthropic/` and `internal/llm/minimax/` packages (replaced by preset entries using `anthropiccompat` directly).
10. README rewrite.

### Risks

- **SDK lag for reasoning fields** (resolves B1 fully). In v3 we dropped the SDK from the wire entirely — stdlib only. `delta.reasoning_content`, `delta.reasoning`, `delta.refusal`, `completion_tokens_details.reasoning_tokens`, and outbound `reasoning` on assistant messages are all native fields on our own request/response structs. Risk fully mitigated; also simplifies the dependency graph.
- **Tool-argument JSON fragmentation.** OpenAI splits tool-call arguments across many chunks (including empty strings). Accumulator defers `EventToolUseStop` until `finish_reason` arrives. Existing agent loop at `loop.go:118` trusts the stop event.
- **Trinity wire shape.** §0 probe is a hard gate. Implementation does not start until the shape is confirmed in this doc.
- **Multi-turn reasoning (§7) is new scope** driven by Trinity's documented requirement. Skipping it would ship a broken Trinity experience. Scope creep accepted.
- **Ollama / vLLM finish_reason omission.** Treat missing/null finish_reason on clean close as `"end_turn"`. Debug-log it.
- **SSE framing quirks.** Committed in §3.5: `Accept-Encoding: identity` on all streaming requests.
- **`context_length_exceeded` 400s.** OpenAI returns `{"error":{"code":"context_length_exceeded", …}}`. The non-200 decoder in §3.5 surfaces the error verbatim, so users see a readable message and know to `/compact`. No special case needed; log distinctly at debug.
- **Breaking config (M3).** One README migration snippet. Pre-1.0, tiny user base, acceptable.

### Deferred / explicitly not in scope

- Responses API.
- `strict: true` on function defs (GPT-4o/GPT-5 guaranteed-valid JSON).
- `seed`, `logprobs`, `n > 1`, `response_format`, `service_tier`, `stop`.
- `tool_choice: "required"` / specific-tool forcing.
- Per-request runtime `/reasoning <level>` override.
- Anthropic extended-thinking round-trip (requires signed thinking blocks; see §7 Scope).
- Azure OpenAI support (`api-key` header is wired via `caps.AuthHeader`, but Azure's per-deployment URL structure + `api-version` query param is not covered — add a dedicated `azure` wire later if needed).
- Retry / backoff on `429` (surface `Retry-After` in the error message for now; no automatic retry).
- Deprecation shim for `internal/llm/anthropic` / `internal/llm/minimax` imports (reviewer R1-S1 — rejected as YAGNI for pre-1.0 with no downstream importers).

---

## Follow-ups (explicitly out of scope)

- Responses API support (add per-entry `api = "responses"`).
- Automatic pricing / cost display.
- In-TUI provider entry editor.
- OpenAI batch, files, audio, or image inputs.
- `/reasoning <level>` runtime command (thin wrapper over per-model override — trivial to add later).
