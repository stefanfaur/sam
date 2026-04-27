package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/clipboard"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/openaicompat"
)

// imageAttachedMsg carries a freshly preprocessed clipboard image to be
// appended to m.imageAttachments on the main update loop.
type imageAttachedMsg struct{ att imageAttachment }

// imageAttachErrMsg surfaces a paste failure (no image, vision unsupported,
// preprocess error). The Update handler renders it via addInfo.
type imageAttachErrMsg struct{ err error }

// cmdPasteImage reads the clipboard, validates vision support, preprocesses,
// and returns a tea.Cmd that emits either imageAttachedMsg or imageAttachErrMsg.
func (m *Model) cmdPasteImage() tea.Cmd {
	provider := m.status.provider
	model := m.status.model
	wire := ""
	if entry, ok := m.providers[provider]; ok {
		wire = entry.Wire
	}
	return func() tea.Msg {
		if !visionSupported(wire, model) {
			return imageAttachErrMsg{err: fmt.Errorf(
				"model %q (%s) doesn't support images. Vision-capable: %s",
				model, provider, joinModels(visionCapableModels()))}
		}
		data, mediaType, err := clipboard.ReadImageFromClipboard()
		if err != nil {
			return imageAttachErrMsg{err: err}
		}
		if err := clipboard.ValidateImageMIME(mediaType); err != nil {
			return imageAttachErrMsg{err: err}
		}
		processed, err := preprocessImage(data, mediaType)
		if err != nil {
			return imageAttachErrMsg{err: err}
		}
		tokens := estimateTokens(processed.width, processed.height, provider)
		return imageAttachedMsg{att: imageAttachment{
			data:      processed.data,
			mediaType: processed.mediaType,
			width:     processed.width,
			height:    processed.height,
			tokens:    tokens,
		}}
	}
}

// joinModels concatenates a model list for the refusal message.
func joinModels(models []string) string {
	if len(models) == 0 {
		return "(none configured)"
	}
	out := ""
	for i, m := range models {
		if i > 0 {
			out += ", "
		}
		out += m
	}
	return out
}

// visionSupported reports whether (wire, model) accepts image content blocks.
// wire is the provider's transport ("anthropic" or "openai"); model is the
// model identifier as configured.
func visionSupported(wire, model string) bool {
	switch wire {
	case "anthropic":
		return llm.AnthropicVisionSupported(model)
	case "openai", "openai-compat":
		return openaicompat.DefaultCaps(model).Vision
	default:
		// Unknown wire: be conservative and refuse so we don't silently
		// burn tokens against a provider that may reject the payload.
		return false
	}
}

// visionCapableModels enumerates well-known vision-capable model names for
// inclusion in the refusal message. The list is hand-curated from the
// Anthropic and OpenAI capability registries.
func visionCapableModels() []string {
	out := []string{}
	for _, m := range []string{
		"claude-3-opus", "claude-3-5-sonnet", "claude-3-5-haiku",
		"claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-4-5",
	} {
		if llm.AnthropicVisionSupported(m) {
			out = append(out, m)
		}
	}
	for _, m := range []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-4.1"} {
		if openaicompat.DefaultCaps(m).Vision {
			out = append(out, m)
		}
	}
	return out
}
