package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type WriteInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=Absolute path."`
	Content  string `json:"content" jsonschema:"required,description=Full file contents to write."`
}

var writeDescription = "Write content to a file. The file must be absolute path."

func NewWrite(tracker *ReadTracker) Tool {
	return New[WriteInput]("Write", writeDescription, func(ctx context.Context, in WriteInput) (Result, error) {
		return runWrite(ctx, in, tracker)
	})
}

func runWrite(ctx context.Context, in WriteInput, tracker *ReadTracker) (Result, error) {
	if !filepath.IsAbs(in.FilePath) {
		return Result{Output: "file_path must be absolute", IsError: true}, nil
	}

	// Check if file exists and was not read first
	if tracker != nil {
		info, err := os.Stat(in.FilePath)
		if err == nil && !tracker.Seen(in.FilePath) {
			return Result{Output: fmt.Sprintf("refuse overwrite: Read file first (file exists, %d bytes)", info.Size()), IsError: true}, nil
		}
	}

	// Check parent directory exists
	parent := filepath.Dir(in.FilePath)
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		return Result{Output: "parent directory does not exist", IsError: true}, nil
	}

	// Write file
	if err := os.WriteFile(in.FilePath, []byte(in.Content), 0644); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}

	// Mark as read (contents now known)
	if tracker != nil {
		tracker.Mark(in.FilePath)
	}

	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.FilePath)}, nil
}
