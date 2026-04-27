# Codebase Map
> Last updated: 2026-04-27 · "SAM CLI agent with Go stack, Bubbletea TUI, multi-provider LLM support (Anthropic, OpenAI compatible), per-tool approval policies, and per-model system-prompt composition. Family map split 6→9 (gpt/gpt-reasoning, deepseek/deepseek-reasoner/deepseek-v4); base prompt rewritten with SAFETY/SCOPE/OUTPUT; openaicompat gained PrependFormatting cap, system-as-user splice, and tool-call empty-content fence; docs/families/*.md hosts maintainer reference per family. Hybrid Esc/Esc-Esc cancel scope (granular vs abort) plus mid-stream steer queue: Enter during pending turn queues input, drained at clean iteration boundary as a trailing text block on the tool_results user message, with end_turn auto-submit and provider-error preservation. Feature D shipped: @file picker overlay (sahilm/fuzzy + git ls-files / recursive walk), Ctrl+V binary clipboard image attach (golang.design/x/clipboard, CGO), @image:/path syntax, Vision capability registry (openaicompat Vision flag + llm.AnthropicVisionSupported), ContentImage on llm.Message + ImageAttachment, Agent.SubmitWithAttachments, multimodal translation for both wires (anthropic.NewImageBlockBase64 + openaicompat chatMessage.MarshalJSON content-array), and an internal/image preprocess pipeline (Lanczos resize 1568px, PNG/JPEG selection, EXIF strip). Feature B shipped: Esc-Esc rewind via per-turn shadow git snapshots under refs/sam/checkpoints/<session>/<turn> using go-git/v5 (HEAD/branch/index backed up + restored so user state stays intact). Durable JSON sidecar at $XDG_STATE_HOME/sam/sessions/<sid>.json. New internal/checkpoint package (Manager.Snapshot/Restore/ListPaths/OverlapPaths/MaxTurn/DeleteSession + SessionID + IsGitRepo + SweepOrphans). Agent gained SessionID/Checkpoint options, History/SetHistory/SetTurnCount/Checkpoint accessors, snapshotTurn graceful-completion hook (cancelled turns never snapshot). TUI rewindPicker + restoreConfirm full-screen split components, Esc-Esc idle opens picker, restore-mode chooser ([c] conv-only / [b] conv+code) with overlap protection (! marker on user-edited paths), Ctrl+Z + /unrewind undo via one-slot buffer cleared on next submit. main.go mints SessionID, runs orphan sweep, defers ref+sidecar cleanup on exit. New [ux] config block: checkpoint_enabled (default true), esc_double_window_ms (default 500)."

## Physical Modules
- **cmd/sam**             → CLI entry point, config loading, provider initialization, resolveSystemPrompt
- **internal/agent**      → Core agent loop, message history, tool submission and streaming, SetSystem for mid-session prompt swap, SubmitWithAttachments for multimodal user turns
- **internal/config**     → TOML-based config parsing, provider presets, model defaults, PromptFamily type with longest-prefix FamilyForModel resolver
- **internal/llm**        → Provider interface, types, registry, Anthropic + OpenAI-compatible wires
- **internal/logging**    → Structured JSON logging to file and in-memory ring buffer
- **internal/policy**     → Per-tool approval rules, allowlist/denylist enforcement
- **internal/rtk**        → Optional transparent compression layer for Bash and Read tools
- **internal/skills**     → Skill discovery, YAML frontmatter parsing, skill execution
- **internal/system**     → Embedded + disk system prompt, tool descriptions, per-family prompt addenda (prompts/<family>.md), seed manifest
- **internal/tools**      → Read, Write, Edit, Bash tool implementations
- **internal/tui**        → Bubbletea-based terminal UI, settings modal, debug overlay, SystemResolverFn plumbing, @file picker, image paste/badge, @image extraction
- **internal/image**      → Lanczos resize, format selection, metadata strip (used at image attach time)
- **internal/clipboard**  → Binary clipboard image read (CGO via golang.design/x/clipboard), MIME validation
- **internal/checkpoint**  → Per-turn shadow git snapshots (go-git/v5) under refs/sam/checkpoints/, surgical Restore + overlap detection + orphan sweep, used by Esc-Esc rewind

## Tech Stack
- **Language:** Go 1.26.2
- **TUI Framework:** Charmbracelet (Bubbletea, Bubbles, Glamour, Huh, Lipgloss)
- **Config Format:** TOML (BurntSushi)
- **LLM SDKs:** Anthropic SDK (official), OpenAI-compatible wire
- **JSON Schema:** invopop/jsonschema
- **Utilities:** YAML parsing, clipboard, colorization, formatting

## Logical Domains → Detail Files
- **providers** → thoughts/shared/index/providers.md
- **agent** → thoughts/shared/index/agent.md
- **tools** → thoughts/shared/index/tools.md
- **config** → thoughts/shared/index/config.md
- **system** → thoughts/shared/index/system.md
- **ui** → thoughts/shared/index/ui.md
- **checkpoint** → thoughts/shared/index/checkpoint.md
