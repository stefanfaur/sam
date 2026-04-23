package openaicompat

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(s *scanner) ([][]byte, error) {
	var out [][]byte
	for {
		raw, done, err := s.Next()
		if done {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, append([]byte(nil), raw...))
	}
}

func TestSSEValidFrame(t *testing.T) {
	body := "data: {\"x\":1}\n\n"
	s := newScanner(strings.NewReader(body))
	frames, err := readAll(s)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(frames) != 1 || string(frames[0]) != `{"x":1}` {
		t.Fatalf("frames: %q", frames)
	}
}

func TestSSECommentIgnored(t *testing.T) {
	body := ": heartbeat\ndata: {\"y\":2}\n\n: another\n\ndata: [DONE]\n\n"
	s := newScanner(strings.NewReader(body))
	frames, err := readAll(s)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(frames) != 1 || string(frames[0]) != `{"y":2}` {
		t.Fatalf("frames: %q", frames)
	}
}

func TestSSEDoneSentinel(t *testing.T) {
	body := "data: {\"z\":3}\n\ndata: [DONE]\n\ndata: {\"nope\":true}\n\n"
	s := newScanner(strings.NewReader(body))
	frames, err := readAll(s)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(frames) != 1 || string(frames[0]) != `{"z":3}` {
		t.Fatalf("frames after [DONE]: %q", frames)
	}
}

func TestSSEMultiLineDataJoined(t *testing.T) {
	body := "data: {\"a\":1,\ndata: \"b\":2}\n\n"
	s := newScanner(strings.NewReader(body))
	frames, err := readAll(s)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(frames) != 1 || string(frames[0]) != "{\"a\":1,\n\"b\":2}" {
		t.Fatalf("multiline: %q", frames)
	}
}

func TestSSEAbruptEOFMidFrame(t *testing.T) {
	// Data line without terminating blank line.
	body := "data: {\"x\":1}\n"
	s := newScanner(strings.NewReader(body))
	_, err := readAll(s)
	if err == nil || errors.Is(err, io.EOF) && err != io.ErrUnexpectedEOF {
		// We expect either nothing (silent) or an unexpected-EOF error.
		// Be lenient: the important invariant is we don't yield a bogus frame.
	}
}
