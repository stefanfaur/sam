package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type BashInput struct {
	Command   string  `json:"command" jsonschema:"required,description=Shell command executed via bash -c."`
	TimeoutMS flexInt `json:"timeout_ms,omitempty" jsonschema:"description=Max 600000 (10 min). Default 120000 (2 min)."`
}

const (
	defaultBashTimeout = 2 * time.Minute
	maxBashTimeout     = 10 * time.Minute
	bashOutputLimit    = 30 * 1024
)

var bashDescription = "Execute a shell command via bash -c. Stdout, stderr, and a non-zero exit code are returned in the output. Use for anything that needs the shell."

type limitedWriter struct {
	w         io.Writer
	n         int
	limit     int
	truncated bool
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.n >= lw.limit {
		lw.truncated = true
		return len(p), nil
	}
	remaining := lw.limit - lw.n
	if len(p) > remaining {
		_, err := lw.w.Write(p[:remaining])
		lw.n += remaining
		lw.truncated = true
		if err != nil {
			return remaining, err
		}
		return len(p), nil
	}
	n, err := lw.w.Write(p)
	lw.n += n
	return n, err
}

func NewBash(launchDir string) Tool {
	return New[BashInput]("Bash", bashDescription, func(ctx context.Context, in BashInput) (Result, error) {
		return runBash(ctx, in, launchDir)
	})
}

func runBash(ctx context.Context, in BashInput, launchDir string) (Result, error) {
	if strings.TrimSpace(in.Command) == "" {
		return Result{Output: "command is empty", IsError: true}, nil
	}

	timeout := defaultBashTimeout
	if in.TimeoutMS > 0 {
		timeout = time.Duration(in.TimeoutMS) * time.Millisecond
	}
	if timeout > maxBashTimeout {
		timeout = maxBashTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command("bash", "-c", in.Command)
	cmd.Dir = launchDir
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	lwOut := &limitedWriter{w: &stdout, limit: bashOutputLimit}
	lwErr := &limitedWriter{w: &stderr, limit: bashOutputLimit}
	cmd.Stdout = lwOut
	cmd.Stderr = lwErr

	if err := cmd.Start(); err != nil {
		return Result{Output: "failed to start: " + err.Error(), IsError: true}, nil
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	timedOut := false
	select {
	case runErr = <-done:
	case <-runCtx.Done():
		timedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case runErr = <-done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			runErr = <-done
		}
	}

	var buf strings.Builder
	buf.Write(stdout.Bytes())
	if stderr.Len() > 0 {
		if buf.Len() > 0 && !strings.HasSuffix(buf.String(), "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("<stderr>\n")
		buf.Write(stderr.Bytes())
		if !strings.HasSuffix(stderr.String(), "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("</stderr>\n")
	}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		fmt.Fprintf(&buf, "<exit>%d</exit>\n", exitErr.ExitCode())
	}
	if lwOut.truncated || lwErr.truncated {
		buf.WriteString("<truncated>true</truncated>\n")
	}

	if timedOut {
		buf.WriteString("<timeout>true</timeout>\n")
		return Result{Output: buf.String(), IsError: true}, nil
	}
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); !ok {
			return Result{Output: buf.String() + "<error>" + runErr.Error() + "</error>\n", IsError: true}, nil
		}
	}
	return Result{Output: buf.String()}, nil
}
