package llm

import (
	"encoding/json"
	"testing"
)

func TestImageContentBlockRoundTrip(t *testing.T) {
	msg := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: ContentText, Text: "look:"},
			{Type: ContentImage, ImageData: "iVBORw0KGgo=", MediaType: "image/png"},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Content) != 2 {
		t.Fatalf("blocks: got %d, want 2", len(got.Content))
	}
	if got.Content[1].Type != ContentImage {
		t.Errorf("type: got %s, want %s", got.Content[1].Type, ContentImage)
	}
	if got.Content[1].ImageData != "iVBORw0KGgo=" {
		t.Errorf("image data not preserved: %q", got.Content[1].ImageData)
	}
	if got.Content[1].MediaType != "image/png" {
		t.Errorf("media type: %q", got.Content[1].MediaType)
	}
}
