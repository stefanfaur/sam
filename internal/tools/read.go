package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ReadInput struct {
	FilePath string  `json:"file_path" jsonschema:"required,description=Absolute path to the file to read."`
	Offset   flexInt `json:"offset,omitempty" jsonschema:"description=1-indexed line number to start from."`
	Limit    flexInt `json:"limit,omitempty" jsonschema:"description=Max number of lines to return. Default 2000."`
}

const (
	defaultReadLimit = 2000
	maxLineWidth     = 2000
)

var readDescription = "Read the contents of a file from disk. Returns the file contents with line numbers. Use this when you need to see the content of a file. Supports offset and limit parameters for large files."

// ReadTracker tracks which files have been read in a session
type ReadTracker struct {
	mu   sync.RWMutex
	seen map[string]struct{}
}

func NewReadTracker() *ReadTracker {
	return &ReadTracker{seen: make(map[string]struct{})}
}

func (r *ReadTracker) Mark(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[p] = struct{}{}
}

func (r *ReadTracker) Seen(p string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.seen[p]
	return ok
}

func NewRead(tracker *ReadTracker) Tool {
	return New[ReadInput]("Read", readDescription, func(ctx context.Context, in ReadInput) (Result, error) {
		return runRead(ctx, in, tracker)
	})
}

func runRead(ctx context.Context, in ReadInput, tracker *ReadTracker) (Result, error) {
	if !filepath.IsAbs(in.FilePath) {
		return Result{Output: "file_path must be absolute", IsError: true}, nil
	}

	f, err := os.Open(in.FilePath)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	defer f.Close()

	// Check if binary
	if isBinary(f) {
		return Result{Output: "binary file; use Bash with the right tool", IsError: true}, nil
	}

	// Mark as read
	if tracker != nil {
		tracker.Mark(in.FilePath)
	}

	_, _ = f.Seek(0, io.SeekStart)

	offset := int(in.Offset)
	if offset < 1 {
		offset = 1
	}
	limit := int(in.Limit)
	if limit <= 0 {
		limit = defaultReadLimit
	}

	var buf strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	written := 0
	for sc.Scan() {
		line++
		if line < offset {
			continue
		}
		if written >= limit {
			break
		}
		txt := sc.Text()
		if len(txt) > maxLineWidth {
			txt = txt[:maxLineWidth] + "...[truncated]"
		}
		fmt.Fprintf(&buf, "%6d\t%s\n", line, txt)
		written++
	}
	if err := sc.Err(); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	return Result{Output: buf.String()}, nil
}

// isBinary checks if a file is binary by reading the first 512 bytes
func isBinary(r io.Reader) bool {
	data := make([]byte, 512)
	n, _ := r.Read(data)
	if n == 0 {
		return false
	}
	// Check for null bytes
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	// Check content type
	ct := http.DetectContentType(data[:n])
	return !strings.HasPrefix(ct, "text/") && ct != "application/json"
}
