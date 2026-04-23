package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type EditInput struct {
	FilePath   string `json:"file_path" jsonschema:"required"`
	OldString  string `json:"old_string" jsonschema:"required,description=Exact text to replace."`
	NewString  string `json:"new_string" jsonschema:"required,description=Replacement text (empty to delete)."`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences instead of requiring uniqueness."`
}

func NewEdit(tracker *ReadTracker, description string) Tool {
	return New[EditInput]("Edit", description, func(ctx context.Context, in EditInput) (Result, error) {
		return runEdit(ctx, in, tracker)
	})
}

func runEdit(ctx context.Context, in EditInput, tracker *ReadTracker) (Result, error) {
	if !filepath.IsAbs(in.FilePath) {
		return Result{Output: "file_path must be absolute", IsError: true}, nil
	}

	// Check no-op
	if in.OldString == in.NewString {
		return Result{Output: "no change needed (old_string == new_string)", IsError: true}, nil
	}

	// Check file was read
	if tracker == nil || !tracker.Seen(in.FilePath) {
		return Result{Output: "file has not been read yet; Read it first", IsError: true}, nil
	}

	// Read file
	content, err := os.ReadFile(in.FilePath)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}

	// Count occurrences
	count := strings.Count(string(content), in.OldString)
	if count == 0 {
		return Result{Output: "old_string not found in file", IsError: true}, nil
	}
	if count > 1 && !in.ReplaceAll {
		return Result{Output: fmt.Sprintf("%d matches found; add context or set replace_all=true", count), IsError: true}, nil
	}

	// Replace
	var newContent string
	if in.ReplaceAll {
		newContent = strings.ReplaceAll(string(content), in.OldString, in.NewString)
	} else {
		newContent = strings.Replace(string(content), in.OldString, in.NewString, 1)
	}

	// Write atomically
	tmp := in.FilePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newContent), 0644); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	if err := os.Rename(tmp, in.FilePath); err != nil {
		os.Remove(tmp)
		return Result{Output: err.Error(), IsError: true}, nil
	}

	replacements := count
	if !in.ReplaceAll {
		replacements = 1
	}

	return Result{Output: fmt.Sprintf("edited %s (%d replacement(s))", in.FilePath, replacements)}, nil
}
