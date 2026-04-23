package openaicompat

import (
	"strings"
	"testing"
)

type tagCase struct {
	name          string
	chunks        []string // default: one chunk = concat of all; feedAllSplits uses concat
	wantText      string
	wantThinking  string
	skipAllSplits bool
}

var tagCases = []tagCase{
	{name: "plain", chunks: []string{"hello world"}, wantText: "hello world"},
	{name: "one tag", chunks: []string{"hi <think>ok</think>bye"}, wantText: "hi bye", wantThinking: "ok"},
	{name: "adjacent tags", chunks: []string{"<think>a</think><think>b</think>"}, wantThinking: "ab"},
	{name: "unclosed", chunks: []string{"pre<think>tail"}, wantText: "pre", wantThinking: "tail"},
	{name: "false positive", chunks: []string{"drink <thirsty> water"}, wantText: "drink <thirsty> water"},
	{name: "double <<think>", chunks: []string{"<<think>x</think>"}, wantText: "<", wantThinking: "x"},
	{name: "split < + tag", chunks: []string{"<", "<think>x</think>"}, wantText: "<", wantThinking: "x", skipAllSplits: true},
	{name: "nested literal", chunks: []string{"<think>a<think>b</think>c"}, wantText: "c", wantThinking: "a<think>b"},
}

func runParser(chunks []string) (string, string) {
	p := NewThinkTagParser()
	var text, thinking strings.Builder
	for _, c := range chunks {
		t, k := p.Feed(c)
		text.WriteString(t)
		thinking.WriteString(k)
	}
	t, k := p.Flush()
	text.WriteString(t)
	thinking.WriteString(k)
	return text.String(), thinking.String()
}

func TestThinkTagCases(t *testing.T) {
	for _, tc := range tagCases {
		t.Run(tc.name, func(t *testing.T) {
			gotT, gotK := runParser(tc.chunks)
			if gotT != tc.wantText {
				t.Errorf("text: got %q want %q", gotT, tc.wantText)
			}
			if gotK != tc.wantThinking {
				t.Errorf("thinking: got %q want %q", gotK, tc.wantThinking)
			}
		})
	}
}

func TestThinkTagAllSplits(t *testing.T) {
	for _, tc := range tagCases {
		if tc.skipAllSplits {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			full := strings.Join(tc.chunks, "")
			for i := 1; i < len(full); i++ {
				chunks := []string{full[:i], full[i:]}
				gotT, gotK := runParser(chunks)
				if gotT != tc.wantText || gotK != tc.wantThinking {
					t.Errorf("split@%d: text=%q thinking=%q (want %q / %q)",
						i, gotT, gotK, tc.wantText, tc.wantThinking)
				}
			}
			// Byte-by-byte (1-byte chunks).
			chunks := make([]string, len(full))
			for i := range full {
				chunks[i] = full[i : i+1]
			}
			gotT, gotK := runParser(chunks)
			if gotT != tc.wantText || gotK != tc.wantThinking {
				t.Errorf("byte-by-byte: text=%q thinking=%q", gotT, gotK)
			}
		})
	}
}
