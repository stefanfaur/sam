package rtk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOn   Mode = "on"
	ModeOff  Mode = "off"
)

const (
	rtkRewriteTimeout = 5 * time.Second
	rtkReadTimeout    = 30 * time.Second
	rtkReadMaxBytes   = 1 * 1024 * 1024
	rtkDetectTimeout  = 2 * time.Second
)

type Client struct {
	mode    Mode
	enabled bool
	version string
	execer  Execer
}

// Execer spawns processes. Mockable in tests.
type Execer interface {
	Command(ctx context.Context, name string, arg ...string) *exec.Cmd
}

type realExecer struct{}

func (realExecer) Command(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, arg...)
}

func New(mode Mode) *Client {
	return &Client{
		mode:   mode,
		execer: realExecer{},
	}
}

// Detect probes the rtk binary and sets enabled/version.
// mode=on: fatal error if binary missing or --version fails.
// mode=auto: silently disables when binary missing.
// mode=off: no probe, stays disabled.
func (c *Client) Detect(ctx context.Context) error {
	if c.mode == ModeOff {
		c.enabled = false
		return nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, rtkDetectTimeout)
	defer cancel()

	cmd := c.execer.Command(probeCtx, "rtk", "--version")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		if c.mode == ModeOn {
			return fmt.Errorf("rtk binary not found or failed: %w (stderr: %s)", err, errOut.String())
		}
		c.enabled = false
		return nil
	}

	c.enabled = true
	c.version = strings.TrimSpace(out.String())
	return nil
}

func (c *Client) Enabled() bool  { return c.enabled }
func (c *Client) Version() string { return c.version }
func (c *Client) Mode() Mode      { return c.mode }

// Rewrite runs `rtk rewrite <cmd>`.
// Exit 0 -> (rewritten, true, nil).
// Exit 1 -> ("", false, nil) rtk has no equivalent.
// Other -> ("", false, err).
func (c *Client) Rewrite(ctx context.Context, cmd string) (string, bool, error) {
	if !c.enabled {
		return "", false, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, rtkRewriteTimeout)
	defer cancel()

	rtkcmd := c.execer.Command(runCtx, "rtk", "rewrite", cmd)
	var out, errOut bytes.Buffer
	rtkcmd.Stdout = &out
	rtkcmd.Stderr = &errOut

	err := rtkcmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, fmt.Errorf("rtk rewrite failed: %v (stderr: %s)", err, strings.TrimSpace(errOut.String()))
	}

	rewritten := strings.TrimSpace(out.String())
	if rewritten == "" {
		return "", false, errors.New("rtk rewrite returned empty output")
	}
	return rewritten, true, nil
}

// Read runs `rtk read --level minimal -n <path>` and returns stdout (cap rtkReadMaxBytes).
func (c *Client) Read(ctx context.Context, path string) ([]byte, error) {
	if !c.enabled {
		return nil, errors.New("rtk client disabled")
	}

	runCtx, cancel := context.WithTimeout(ctx, rtkReadTimeout)
	defer cancel()

	rtkcmd := c.execer.Command(runCtx, "rtk", "read", "--level", "minimal", "-n", path)
	var out, errOut bytes.Buffer
	rtkcmd.Stdout = &out
	rtkcmd.Stderr = &errOut

	if err := rtkcmd.Run(); err != nil {
		return nil, fmt.Errorf("rtk read failed: %v (stderr: %s)", err, strings.TrimSpace(errOut.String()))
	}

	b := out.Bytes()
	if len(b) > rtkReadMaxBytes {
		return nil, fmt.Errorf("rtk read output exceeds %d bytes (%d received); retry with raw: true", rtkReadMaxBytes, len(b))
	}
	return b, nil
}
