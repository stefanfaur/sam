# Per-Family System-Prompt Redesign

**Date:** 2026-04-24
**Status:** Design (awaiting implementation plan)
**Scope:** `internal/system/defaults/*`, `internal/config/prompt_families.go`, `internal/llm/*`

## Goals

- Raise coding-performance ceiling per family by aligning prompts to each vendor's published guidance (Anthropic, OpenAI, Moonshot, DeepSeek, Arcee, Minimax).
- Cut total prompt tokens sent on every request by removing content the model cannot act on (context-size trivia, wire-type labels, API-field names, maintainer notes).
- Keep the universal base prompt minimal and apply equally to every family.

## Non-goals

- No change to the composition pipeline itself (`resolveSystemPrompt`, seed manifest, disk-over-embedded precedence) — already landed.
- No addition of new provider wires or new LLM SDKs.
- No change to the skills, tools, or policy subsystems.

## Principle

System-prompt files contain **instructions the model acts on**. Model metadata (context window, wire transport, API field names, variant lists, known infra bugs) moves to code and reference docs. This rule is applied ruthlessly: if the model cannot change its behavior based on a sentence, that sentence does not belong in the prompt.

## Base prompt rewrite

Path: `internal/system/defaults/system-prompt.md`

Change: drop the standalone `THINKING — MANDATORY` block (was family-specific content masquerading as universal). Merge "verify, don't guess" into evidence block. Add three universal discipline blocks that were duplicated across family drafts or missing entirely: scope control, safety (destructive-action confirmation), and no-preamble. Net size: ~23 lines / ~1120 bytes (up from ~940 of the pre-SAFETY draft, down from the original ~20 lines of mixed content).

Final text:

```
You are SAM, a terse coding agent.

CAVEMAN SPEECH.
Drop articles (a, an, the). Drop filler (just, really, basically, actually, simply).
Drop pleasantries, hedging. Fragments fine. Minimum words.
Technical terms, code, file paths, commits unchanged.
Pattern: [thing] [action] [reason]. [next step].

EVIDENCE OVER ASSERTION.
Claims need proof: cite file:line, run command, show output.
Uncertainty marked with explicit "unverified:" prefix. No other hedging.
Never answer about code you have not read. File referenced = read first.

SCOPE.
Only make changes the user requested. No added abstractions, defensive checks for impossible cases, error handling the existing code doesn't need, or docstrings for unchanged code.

SAFETY.
Destructive actions (rm, drop, force-push, reset --hard, branch delete) need user confirmation before execution. Investigate unknown state before overwriting. Prefer reversible path when one exists.

OUTPUT.
Respond directly. No preamble. Do not open with "Here is...", "Based on...", "Certainly", "I'll...".

Tools: Read, Write, Edit, Bash. Read before Write/Edit on existing files.
```

Rationale for the two deviations from prior draft:
- **Uncertainty reframing.** Hard banning the words "I think" / "probably" / "should work" risks the model dropping appropriate calibration markers and stating unverified claims as fact. Positive framing ("use explicit `unverified:` prefix") preserves calibration while killing filler.
- **SAFETY block added.** SAM ships with the Bash tool. A coding agent without destructive-action guidance is a safety gap — Claude's own production system prompt carries extensive reversibility and confirmation rules. This block also anchors the Claude family's "do not stop early" line so it doesn't override caution on destructive turns.

## Family splits

Current families: `claude`, `minimax`, `kimi-k2`, `trinity`, `gpt`, `deepseek`.

Splits required because each cluster has opposing CoT / tool-call guidance that cannot be reconciled in one prompt:

| New family | Prefixes | Rationale |
|------------|----------|-----------|
| `gpt` | `gpt-4o`, `gpt-4.`, `gpt-4-` | No internal reasoning — persistence helps per OpenAI cookbook. `gpt-4.` catches `gpt-4.1*` and `gpt-4.5*`. Future reasoning variants (if any) need explicit prefix in `gpt-reasoning`. |
| `gpt-reasoning` | `gpt-5`, `o1`, `o3`, `o4` | Internal reasoning — explicit CoT scaffolds documented as counterproductive |
| `deepseek` | `deepseek-` (catch-all) | Legacy V3 / chat / unknown `deepseek-*` variants default here; reasoner and v4 prefixes beat it via longest-prefix |
| `deepseek-reasoner` | `deepseek-reasoner`, `deepseek-r1` | R1-0528+ — RL-trained always-on reasoning on OpenAI-compat wire with `reasoning_content` field; CoT injection hurts |
| `deepseek-v4` | `deepseek-v4` | V4-pro / V4-flash via DeepSeek `/anthropic` endpoint (see `thoughts/shared/plans/providers/2026-04-24-deepseek-v4-design.md`). Thinking-by-default + signed thinking blocks + parallel tool calls + 1M context. Distinct enough from R1 (different wire, different tool-call semantics) and from V3 (reasoning-native vs chat) to warrant its own addendum. |
| `claude`, `minimax`, `kimi-k2`, `trinity` | unchanged | — |

Prefix collision handled by existing longest-prefix resolver (`Config.FamilyForModel` in `internal/config/prompt_families.go:33-60`). Verified routing:

- `gpt-5-mini` → `gpt-reasoning` (matches `gpt-5` only)
- `gpt-4o` / `gpt-4-turbo` / `gpt-4.1-mini` / `gpt-4.5-preview` → `gpt`
- `o3-mini` / `o4-mini` → `gpt-reasoning`
- `deepseek-chat` / `deepseek-coder` / `deepseek-v3` → `deepseek` (catch-all)
- `deepseek-reasoner` → `deepseek-reasoner` (len 17 beats `deepseek-` len 9)
- `deepseek-r1-distill-llama-70b` → `deepseek-reasoner`
- `deepseek-v4-pro` / `deepseek-v4-flash` → `deepseek-v4` (len 11 beats `deepseek-` len 9; `deepseek-reasoner` / `deepseek-r1` do not match v4)

## Per-family prompts

All files behavioral-only. No context sizes, no wire labels, no API field names, no maintainer notes.

### `prompts/claude.md`
```
<use_parallel_tool_calls>
If you intend to call multiple tools and there are no dependencies between them, call all independent tools in parallel in a single response. Sequential only when one tool's output feeds the next. Never use placeholders or guess missing parameters.
</use_parallel_tool_calls>

Do not stop tasks early due to token budget concerns. Continue until the request is resolved — SAFETY confirmation pauses still apply for destructive actions.
```

Source: Anthropic `claude-4-best-practices` — verbatim parallel-tool XML snippet measured at ~100% parallel usage; context-compaction line addresses Claude 4.5/4.6/Haiku-4.5 self-truncation.

Dropped from prior draft: "Use extended thinking selectively..." Extended-thinking budget is gated at API-call level via `budget_tokens`, not at prompt level. Base SCOPE + OUTPUT already discourage over-elaboration. The line was borderline by the spec's own "acted-on" principle.

Added to the context-compaction line: SAFETY cross-reference, so "don't stop early" does not override destructive-action confirmation from the base block.

### `prompts/minimax.md`
```
For trivial operations (single file read, single shell command, direct edits), act without deliberation. Thinking overhead on simple tasks adds real latency here.

Prefer batching related work into a single turn. Continuity across turns is limited — what matters within a task should happen in one turn.
```

Source: Minimax M2.7 best-practices docs + GitHub issue #77 — 325x latency overhead on trivial tasks; intentional cross-turn thinking discard.

### `prompts/kimi-k2.md`
```
Lean into tools for verification. Read files, run commands, inspect output. Prefer tool use over speculation on any factual claim about the codebase.

Trust the provided tool list — do not narrate which tool you're picking or why. Select and call directly.
```

Source: Moonshot agent guide explicitly prohibits tool-selection narration; K2 trained on 200-300 sequential tool chains.

### `prompts/trinity.md`
```
Never emit <think>, <thinking>, or similar reasoning tags in any turn. Reasoning stays internal; only ship the final answer and tool calls.
```

Source: Trinity-Large-Thinking emits `<think>` blocks that must not leak into user-visible output. Preview variant emits none. Tag-explicit phrasing is unambiguous about which surface strings to suppress, where the earlier "internal reasoning blocks in your final response" wording left ambiguity about non-final turns.

### `prompts/gpt.md` (chat family)
```
Keep going until the request is fully resolved. Do not yield control mid-task or ask clarifying questions when the answer can be discovered by reading code.

When a user message contains a long document or large context followed by a short trailing instruction, treat the trailing instruction as primary intent.
```

Source: OpenAI GPT-4.1 prompting guide — persistence + sandwich pattern. The guide's third component (narrated in-turn planning) conflicts with base's caveman + no-preamble rules; dropped deliberately. Base's terse-output constraint takes precedence over the marginal SWE-bench gain that narrated planning adds on top of persistence.

Dropped from prior draft: "Place long documents or large contexts before the query. When a long input is followed by instructions, restate the key instruction after it." That guidance addresses the prompt author composing user messages, not the model processing them. The model cannot restructure a user message it has already received, so the sentence was dead weight by the spec's own "acted-on" principle. The replacement phrasing is model-actionable: it tells the model how to weight trailing instructions when the sandwich pattern *does* appear in the user's message.

### `prompts/gpt-reasoning.md` (reasoning family — NEW FILE)
```
Your reasoning happens internally. Output only the final answer and tool calls.

When emitting multiple tool calls in one turn, verify they have no sequencing dependency. Calls that depend on each other's results must go in separate turns.

Only call tools from the provided list. Never promise a future call — if a tool is needed, emit it now.
```

Phrasing softened: base's no-preamble rule already forbids narration, so explicit "do not narrate planning" would be redundant and has a historical side effect of the model interpreting it as an instruction that it HAS a planning faculty to suppress.

Source: OpenAI reasoning-best-practices doc warns CoT prompting degrades o-series; Codex issue #14485 documents git-add/commit parallel-call race.

### `prompts/deepseek.md` (V3)
```
Do not narrate step-by-step plans or reflection in your visible output. Keep reasoning terse; visible output is the final answer and tool calls.
```

Source: DeepSeek V3 + R1 docs both warn against forced CoT scaffolds.

### `prompts/deepseek-reasoner.md` (R1-0528+ — NEW FILE)
```
Do not narrate planning or reflection in your visible output. Reasoning is internal. Ship only the final answer and tool calls.

Only call tools from the provided list. Do not fabricate tools. Never promise a future call — emit it now.
```

Source: DeepSeek R1 model card explicitly warns against scaffolding; R1-0528 release notes confirm function-calling support.

### `prompts/deepseek-v4.md` (V4-pro / V4-flash — NEW FILE)
```
Reasoning is internal. Ship only the final answer and tool calls — reasoning should not appear in visible output.

If you intend to call multiple tools and there are no dependencies between them, call all independent tools in parallel in a single response. Sequential only when one tool's output feeds the next.

Long context available — when a question touches a small-to-medium file, read it whole rather than searching fragments. Grep first only when the file is large or the target is unknown.

Only call tools from the provided list. Do not fabricate tools. Never promise a future call — emit it now.
```

Source: DeepSeek V4 provider design (`thoughts/shared/plans/providers/2026-04-24-deepseek-v4-design.md`) documents anthropic-compatible wire, thinking-default reasoning, signed `ContentThinking` blocks on tool-call round-trips, and 1M-token context.

Design notes:
- **Reasoning-internal line** mirrors `deepseek-reasoner` — V4 is thinking-default, so visible-output narration is the same failure mode.
- **Parallel-tool paragraph** mirrors `claude.md` — V4 uses the Messages API with structured tool use, and parallel calls are a real capability on this wire (unlike R1's openai-compat wire where the coordination semantics differ).
- **Context-affordance line** is the only content-sized piece kept, and only because it is genuinely model-actionable: the model decides whether to `Read` a whole file or grep for fragments, and a 1M-token window changes that decision boundary. Raw numbers (1M / 192k / 120k) are kept out of the prompt per the base principle and live in `docs/families/deepseek-v4.md` instead.
- **Tool-list discipline line** mirrors `deepseek-reasoner` — deepseek-family models have a documented tendency to fabricate tool names when the provided list is terse.

Wire-level concerns (signed thinking-block preservation, Anthropic `ContentThinking` pass-through, per-model `MaxTokens=192000` / thinking budget 120k) are handled in `internal/llm/anthropiccompat/*` and `internal/config/config.go` per the provider design doc. They are deliberately absent from the prompt because the model cannot act on them.

## Out-of-prompt changes (infrastructure)

### Wire layer (`internal/llm/*`)

1. **o-series Markdown restoration.** When the model matches `o1*`, `o3*`, `o4*`, or `gpt-5*` prefix, prepend `Formatting re-enabled.\n` to the composed system message before send. This must be the literal first line per OpenAI spec — cannot live in a prompt file because the file appears after the base prompt.

2. **Self-hosted R1 system-as-user splice.** Add optional `system_role` field to model spec (`config.ModelSpec`). Values: `"system"` (default) or `"user"`. When `"user"`, wire splices the composed system text into the first user message wrapped in `<system>...</system>` tags. Required only for self-hosted original R1 weights; official `api.deepseek.com` serves R1-0528 and accepts the system role — default path unchanged.

3. **Minimax streaming warning.** No code change — documented in `docs/families/minimax.md` as a known upstream bug. Users can disable streaming via existing UI toggle.

4. **Trinity tool-call content normalization.** On Trinity models, when assembling assistant messages that carry `tool_calls`, ensure `content` is `""` not `null`. Single-line change in OpenAI-compatible wire.

### Config layer (`internal/config/prompt_families.go`)

Update `DefaultPromptFamilies` in `internal/config/prompt_families.go:18-27`. Current form is a `map[string]PromptFamily`; new entries:

```go
"gpt":               {Prefixes: []string{"gpt-4o", "gpt-4.", "gpt-4-"}},
"gpt-reasoning":     {Prefixes: []string{"gpt-5", "o1", "o3", "o4"}},
"deepseek":          {Prefixes: []string{"deepseek-"}},
"deepseek-reasoner": {Prefixes: []string{"deepseek-reasoner", "deepseek-r1"}},
"deepseek-v4":       {Prefixes: []string{"deepseek-v4"}},
// claude, minimax (MiniMax- capitalization preserved), kimi-k2, trinity unchanged
```

Add `SystemRole` field to `ModelSpec`:

```go
type ModelSpec struct {
    // existing fields...
    SystemRole string `toml:"system_role,omitempty"` // "system" (default) or "user"
}
```

### Seed manifest

Regenerate sha256 entries for every changed prompt file and the three new files (`gpt-reasoning.md`, `deepseek-reasoner.md`, `deepseek-v4.md`). Existing seed logic preserves user edits — users who already customized `claude.md` etc. on disk will not have those overwritten.

**Post-split transition UX.** Users with a pre-split `~/.sam/system/prompts/deepseek.md` on disk will have that file continue to control the `deepseek` family (legacy V3 + catch-all). The new `deepseek-reasoner` and `deepseek-v4` families fall back to embedded text unless the user creates `~/.sam/system/prompts/deepseek-reasoner.md` / `deepseek-v4.md` themselves. Same for `gpt` / `gpt-reasoning`. Document this in the PR description and in release notes so users don't assume their old `deepseek.md` also covers the split families — this matters in practice because `deepseek-v4-pro` is now the bundled default model for the `deepseek` provider preset (`internal/config/presets.go`), so every fresh DeepSeek user lands on the new family immediately.

## Reference documentation (new)

New directory `docs/families/` with one file per family (e.g., `docs/families/claude.md`) carrying the maintainer-facing metadata that was stripped from prompts: wire type, context windows, API field names, recommended sampling params, known upstream bugs, source links. Not seeded, not sent to model — human reference only.

## Testing strategy

- `internal/system/integration_test.go` — extend coverage: assert each of the eight families (`claude`, `minimax`, `kimi-k2`, `trinity`, `gpt`, `gpt-reasoning`, `deepseek`, `deepseek-reasoner`, `deepseek-v4`) resolves a non-empty addendum, assert composition precedence still honors disk-over-embedded and empty-disk-wipe semantics for new files.
- `internal/config/prompt_families_test.go` — add cases: `gpt-5` resolves to `gpt-reasoning`, `gpt-4o` resolves to `gpt`, `gpt-4.1-mini` resolves to `gpt`, `gpt-4.5-preview` resolves to `gpt` (guards the `gpt-4.` catch-all prefix), `deepseek-reasoner` resolves to reasoner family, `deepseek-chat` resolves to `deepseek`, `deepseek-v4-pro` and `deepseek-v4-flash` resolve to `deepseek-v4` (guards that `deepseek-v4` beats the `deepseek-` catch-all and is not swallowed by `deepseek-r1`).
- New test for `system_role="user"` wire splicing: assert system text appears wrapped in `<system>` tags inside first user message and no top-level system message is sent.
- New test for o-series `Formatting re-enabled` prepend: match prefixes (`o1`, `o3`, `o4`, `gpt-5`), assert literal first line of composed system message; assert prepend is NOT applied on `gpt-4o` or `gpt-4.1`.
- Regression test on base-prompt size: assert `len(EmbeddedPrompt) < 1280 bytes` (current baseline is ~1120 bytes after trim and SAFETY block addition; guards against unintended base growth). Budget was raised from 1024 to accommodate the SAFETY block; further growth should require explicit budget review.
- Base-prompt content assertion: assert the embedded base contains the `SAFETY.` block header, so SAFETY cannot be silently removed by future edits.
- `internal/tui/model_switch_system_test.go` already covers SetSystem re-resolution on switch — no change needed, new families flow through automatically.

## Rollout

Single PR. Atomic: all prompt rewrites + family config + wire changes + tests land together. No feature flag — prompts are data, behavior change is the feature.

Pre-merge behavioral smoke. Unit tests cover family routing, wire-level splicing, and prompt-file presence; they do not cover model behavior. Before merge, run an offline eval set of ~20 tasks (roughly 3-4 per core family) covering at minimum:
- Read-and-report (evidence discipline, no hallucinated files)
- Single-file edit (scope discipline, no drive-by refactors)
- Parallel tool calls (Claude, Kimi, DeepSeek-v4 families — all support structured parallel calls on their wire)
- Trailing-instruction sandwich (GPT family)
- Destructive action proposed by prompt (SAFETY — model should request confirmation)
- Tag leakage check (Trinity — no `<think>` in visible output)
- Thinking-block round-trip (DeepSeek-v4 — tool call followed by continuation; verify no "missing signature" error and no reasoning in visible output)

No automated gate; results reviewed manually by the PR author. This catches prompt-behavior regressions that unit tests cannot.

Post-merge: users running `sam` for the first time get the new embedded defaults. Existing users retain their disk-edited `~/.sam/system/` files unless they delete them (seed manifest preserves user edits by design).

## Files touched

Modified:
- `internal/system/defaults/system-prompt.md`
- `internal/system/defaults/prompts/claude.md`
- `internal/system/defaults/prompts/minimax.md`
- `internal/system/defaults/prompts/kimi-k2.md`
- `internal/system/defaults/prompts/trinity.md`
- `internal/system/defaults/prompts/gpt.md`
- `internal/system/defaults/prompts/deepseek.md`
- `internal/system/seed.go` (sha256 manifest regen)
- `internal/config/prompt_families.go` (family list + split prefixes)
- `internal/config/model_spec.go` or equivalent (`SystemRole` field)
- `internal/llm/openai.go` (o-series Formatting prepend, system-as-user splice, Trinity null→"" normalization)
- `internal/system/integration_test.go`
- `internal/config/prompt_families_test.go`

New:
- `internal/system/defaults/prompts/gpt-reasoning.md`
- `internal/system/defaults/prompts/deepseek-reasoner.md`
- `internal/system/defaults/prompts/deepseek-v4.md`
- `docs/families/*.md` (one file per family, maintainer reference — including `docs/families/deepseek-v4.md` for the V4-specific wire/context/thinking numbers)
- `internal/llm/openai_system_role_test.go` (or extension of existing wire tests)

## Open questions

None blocking. Decisions committed:
- `o-series` Formatting-prepend prefix match lives in the wire (`internal/llm/openai*.go`) using the same prefix list as `gpt-reasoning` family, not a family attribute. Keeps `config.PromptFamily` as pure data; wire owns wire concerns.
- Trinity null→"" normalization is worth landing even if the existing wire already normalizes elsewhere — make it explicit in the assistant-message builder so future refactors don't regress it.

Implementation plan should verify:
- Exact model-spec field name for `SystemRole` matches existing naming convention in `internal/config`.
