package tui

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/llm"
)

// TestStartTurnPassesAttachmentsToAgent verifies that imageAttachments staged
// on the Model end up as a ContentImage block on the wire-bound user message.
func TestStartTurnPassesAttachmentsToAgent(t *testing.T) {
	m, _, startProv, _ := newTestModelWithResolver(t, nil)

	raw := []byte{0xde, 0xad, 0xbe, 0xef}
	m.imageAttachments = []imageAttachment{{
		data:      raw,
		mediaType: "image/png",
		width:     32,
		height:    32,
		tokens:    100,
	}}

	model, _ := m.startTurn("look at this image")
	mm := model.(*Model)
	if mm.imageAttachments != nil {
		t.Errorf("imageAttachments not cleared after startTurn: %v", mm.imageAttachments)
	}
	if mm.pending == nil {
		t.Fatal("pending turn not set")
	}

	// Drain the agent stream so the request is registered on the fake provider.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-mm.pending.events:
			if !ok {
				goto done
			}
		case <-timeout:
			t.Fatal("timed out draining pending events")
		}
	}
done:

	if len(startProv.Calls) == 0 {
		t.Fatal("provider received no calls")
	}
	req := startProv.Calls[len(startProv.Calls)-1]
	if len(req.Messages) == 0 {
		t.Fatal("request has no messages")
	}
	user := req.Messages[0]
	if user.Role != llm.RoleUser {
		t.Fatalf("first message not user: %s", user.Role)
	}
	if len(user.Content) != 2 {
		t.Fatalf("expected text + image content blocks, got %d", len(user.Content))
	}
	if user.Content[0].Type != llm.ContentText || user.Content[0].Text != "look at this image" {
		t.Errorf("text block: %+v", user.Content[0])
	}
	img := user.Content[1]
	if img.Type != llm.ContentImage {
		t.Errorf("expected image content, got %s", img.Type)
	}
	if img.MediaType != "image/png" {
		t.Errorf("media type: %q", img.MediaType)
	}
	want := base64.StdEncoding.EncodeToString(raw)
	if img.ImageData != want {
		t.Errorf("image data:\n got %q\nwant %q", img.ImageData, want)
	}
}

// TestStartTurnWithImageRefExtractsAndStripsToken verifies that an
// @image:/path token in the input is consumed: the cleaned text excludes it,
// and an attachment is queued for the agent. The image file is created in a
// temp dir as a real PNG so the preprocess pipeline can decode it.
func TestStartTurnWithImageRefExtractsAndStripsToken(t *testing.T) {
	m, _, startProv, _ := newTestModelWithResolver(t, nil)

	// Write a tiny valid PNG to a temp path.
	imgPath := writeTestPNG(t)

	model, _ := m.startTurn("describe @image:" + imgPath + " please")
	mm := model.(*Model)
	if mm.pending == nil {
		t.Fatal("pending turn not set")
	}

	// Drain.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-mm.pending.events:
			if !ok {
				goto done
			}
		case <-timeout:
			t.Fatal("timed out draining pending events")
		}
	}
done:

	if len(startProv.Calls) == 0 {
		t.Fatal("provider received no calls")
	}
	req := startProv.Calls[len(startProv.Calls)-1]
	user := req.Messages[0]
	if got := user.Content[0].Text; got != "describe please" {
		t.Errorf("cleaned text: got %q, want %q", got, "describe please")
	}
	hasImage := false
	for _, b := range user.Content {
		if b.Type == llm.ContentImage {
			hasImage = true
		}
	}
	if !hasImage {
		t.Errorf("expected image content block in user message: %+v", user.Content)
	}
}

// writeTestPNG writes a minimal 8x8 PNG to a temp file and returns the path.
func writeTestPNG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/in.png"
	data := minimalPNG()
	if err := writeFile(path, data); err != nil {
		t.Fatalf("write png: %v", err)
	}
	return path
}
