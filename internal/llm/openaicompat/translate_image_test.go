package openaicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
)

func TestTranslateUserMessageWithImageEmitsMultiContent(t *testing.T) {
	msg := llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentText, Text: "describe this"},
			{Type: llm.ContentImage, ImageData: "AAA=", MediaType: "image/png"},
		},
	}
	got := translateUserMessage(msg)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Content != nil {
		t.Errorf("Content (string) should be nil when MultiContent set")
	}
	if len(got[0].MultiContent) != 2 {
		t.Fatalf("MultiContent: got %d parts, want 2", len(got[0].MultiContent))
	}
	if got[0].MultiContent[0].Type != "text" || got[0].MultiContent[0].Text != "describe this" {
		t.Errorf("text part: %+v", got[0].MultiContent[0])
	}
	if got[0].MultiContent[1].Type != "image_url" {
		t.Errorf("image part type: %s", got[0].MultiContent[1].Type)
	}
	if got[0].MultiContent[1].ImageURL == nil ||
		!strings.HasPrefix(got[0].MultiContent[1].ImageURL.URL, "data:image/png;base64,AAA") {
		t.Errorf("image url: %+v", got[0].MultiContent[1].ImageURL)
	}
}

func TestTranslateUserMessageNoImageRetainsStringContent(t *testing.T) {
	msg := llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentText, Text: "plain text"},
		},
	}
	got := translateUserMessage(msg)
	if len(got) != 1 {
		t.Fatalf("messages: %d", len(got))
	}
	if got[0].Content == nil || *got[0].Content != "plain text" {
		t.Errorf("Content: %v", got[0].Content)
	}
	if len(got[0].MultiContent) != 0 {
		t.Errorf("MultiContent should be empty: %v", got[0].MultiContent)
	}
}

func TestChatMessageMarshalsImageContentAsArray(t *testing.T) {
	msg := chatMessage{
		Role: "user",
		MultiContent: []contentPart{
			{Type: "text", Text: "look"},
			{Type: "image_url", ImageURL: &contentPartImage{URL: "data:image/png;base64,XX"}},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"content":[`) {
		t.Errorf("expected content array, got %s", s)
	}
	if !strings.Contains(s, `"image_url":{"url":"data:image/png;base64,XX"}`) {
		t.Errorf("missing image_url field, got %s", s)
	}
}

func TestChatMessageMarshalsStringContent(t *testing.T) {
	c := "hello"
	msg := chatMessage{Role: "user", Content: &c}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"role":"user","content":"hello"}` {
		t.Errorf("got %s", data)
	}
}
