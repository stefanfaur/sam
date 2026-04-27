package agent

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

func TestSubmitWithAttachmentsAttachesImageBlock(t *testing.T) {
	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "saw it"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})

	a := New(Options{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
	})
	a.Start()
	defer a.Close()

	raw := []byte{0x89, 0x50, 0x4e, 0x47}
	atts := []llm.ImageAttachment{
		{Data: raw, MediaType: "image/png", Width: 32, Height: 32},
	}
	if _, err := collectEvents(a.SubmitWithAttachments(context.Background(), "what is this?", atts)); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if len(a.history) == 0 {
		t.Fatal("history empty")
	}
	user := a.history[0]
	if user.Role != llm.RoleUser {
		t.Fatalf("first message role: %s", user.Role)
	}
	if len(user.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(user.Content))
	}
	if user.Content[0].Type != llm.ContentText || user.Content[0].Text != "what is this?" {
		t.Errorf("text block: %+v", user.Content[0])
	}
	img := user.Content[1]
	if img.Type != llm.ContentImage {
		t.Errorf("expected image block, got %s", img.Type)
	}
	if img.MediaType != "image/png" {
		t.Errorf("media type: %s", img.MediaType)
	}
	wantData := base64.StdEncoding.EncodeToString(raw)
	if img.ImageData != wantData {
		t.Errorf("image data mismatch:\n got %q\nwant %q", img.ImageData, wantData)
	}
}
