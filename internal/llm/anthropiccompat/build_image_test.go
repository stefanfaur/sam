package anthropiccompat

import (
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
)

func TestBuildMessagesWithImageBlock(t *testing.T) {
	in := []llm.Message{
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentText, Text: "what's this"},
				{Type: llm.ContentImage, ImageData: "AAA=", MediaType: "image/png"},
			},
		},
	}
	got := BuildMessages(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	// MessageParam carries Content as ContentBlockParamUnion slice; verify the
	// second block is an image with the right base64 + media type.
	user := got[0]
	if len(user.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(user.Content))
	}
	img := user.Content[1].OfImage
	if img == nil {
		t.Fatal("second block is not an image")
	}
	if img.Source.OfBase64 == nil {
		t.Fatal("image source.OfBase64 is nil")
	}
	if string(img.Source.OfBase64.MediaType) != "image/png" {
		t.Errorf("media type: %q", img.Source.OfBase64.MediaType)
	}
	if img.Source.OfBase64.Data != "AAA=" {
		t.Errorf("data: %q", img.Source.OfBase64.Data)
	}
}

func TestBuildMessagesSkipsImageWithMissingFields(t *testing.T) {
	in := []llm.Message{
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentText, Text: "hello"},
				{Type: llm.ContentImage, ImageData: "", MediaType: "image/png"},
			},
		},
	}
	got := BuildMessages(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if len(got[0].Content) != 1 {
		t.Fatalf("expected 1 content block (image dropped), got %d", len(got[0].Content))
	}
}
