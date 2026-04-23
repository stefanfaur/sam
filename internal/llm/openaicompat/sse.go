package openaicompat

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// scanner reads an SSE stream and yields the raw JSON payload of each
// `data:` frame, stopping on `data: [DONE]`. Comment lines (`: ...`) and
// non-`data:` field lines are dropped.
type scanner struct {
	r   *bufio.Reader
	buf bytes.Buffer // holds the current frame's data payload
	eof bool
}

const scannerBufSize = 1 << 20 // 1 MiB — reasoning replies can be long

func newScanner(rd io.Reader) *scanner {
	return &scanner{r: bufio.NewReaderSize(rd, scannerBufSize)}
}

// Next returns the next decoded frame payload, or (nil, true, nil) when the
// stream ends via [DONE] or clean EOF. Malformed bytes bubble up as err.
func (s *scanner) Next() (raw []byte, done bool, err error) {
	if s.eof {
		return nil, true, nil
	}
	for {
		line, err := s.r.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, false, err
		}
		if err == io.EOF && line == "" {
			// Clean EOF with no trailing blank — treat as end-of-stream.
			s.eof = true
			if s.buf.Len() > 0 {
				out := s.buf.Bytes()
				s.buf.Reset()
				return out, false, nil
			}
			return nil, true, nil
		}
		// Strip the trailing newline (and optional \r).
		line = strings.TrimRight(line, "\n")
		line = strings.TrimRight(line, "\r")

		if line == "" {
			// End of frame. Emit accumulated buffer if non-empty.
			if s.buf.Len() == 0 {
				continue
			}
			payload := append([]byte(nil), s.buf.Bytes()...)
			s.buf.Reset()
			if string(payload) == "[DONE]" {
				s.eof = true
				return nil, true, nil
			}
			return payload, false, nil
		}
		if strings.HasPrefix(line, ":") {
			continue // comment
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimPrefix(line, "data:")
			payload = strings.TrimPrefix(payload, " ")
			if s.buf.Len() > 0 {
				s.buf.WriteByte('\n')
			}
			s.buf.WriteString(payload)
			continue
		}
		// Ignore other SSE fields (event:, id:, retry:).
	}
}
