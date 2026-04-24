package tools

import (
	"context"
	"testing"
)

type panicInput struct {
	X string `json:"x,omitempty"`
}

func TestTypedRunRecoversPanicToErrorResult(t *testing.T) {
	tool := New[panicInput]("panicker", "", func(ctx context.Context, _ panicInput) (Result, error) {
		panic("boom")
	})
	res, err := tool.Run(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true on panic")
	}
	if res.Output == "" {
		t.Error("expected non-empty Output on panic")
	}
}
