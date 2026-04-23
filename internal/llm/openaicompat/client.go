package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/stefanfaur/sam/internal/llm"
)

// Client is a thin HTTP transport over POST /chat/completions against any
// OpenAI-compatible endpoint. It does not retry; mid-stream retries are
// non-idempotent and risk duplicated tokens or tool calls.
type Client struct {
	apiKey         string
	baseURL        string
	caps           Capabilities
	parseThinkTags bool
	effortFor      func(model string) string
	httpClient     *http.Client
}

type ClientOptions struct {
	APIKey         string
	BaseURL        string
	Caps           Capabilities
	ParseThinkTags bool
	EffortResolver func(model string) string
	HTTPClient     *http.Client
}

func NewClient(opts ClientOptions) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		// Stream requests must not enforce a client-wide timeout.
		hc = &http.Client{Transport: http.DefaultTransport}
	}
	return &Client{
		apiKey:         opts.APIKey,
		baseURL:        strings.TrimSuffix(opts.BaseURL, "/"),
		caps:           opts.Caps,
		parseThinkTags: opts.ParseThinkTags,
		effortFor:      opts.EffortResolver,
		httpClient:     hc,
	}
}

// Stream issues the chat-completions call and returns a channel of events.
// On non-200 responses the channel yields a single EventError and closes.
// Context cancellation aborts the in-flight read and closes the channel.
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("openaicompat: base URL empty")
	}
	caps := c.caps
	if caps.AuthHeader == "" {
		caps.AuthHeader = "bearer"
	}
	if caps.AuthHeader != "none" && c.apiKey == "" {
		return nil, fmt.Errorf("openaicompat: no API key")
	}

	effort := ""
	if c.effortFor != nil {
		effort = c.effortFor(req.Model)
	}
	body := buildRequest(req, caps, effort)
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Accept-Encoding", "identity")
	switch caps.AuthHeader {
	case "api-key":
		httpReq.Header.Set("api-key", c.apiKey)
	case "none":
		// no auth
	default:
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		errEv := decodeErrorResponse(resp)
		_ = resp.Body.Close()
		ch := make(chan llm.StreamEvent, 1)
		ch <- errEv
		close(ch)
		return ch, nil
	}

	// Decode SSE frames → streamChunks → translate → caller.
	chunks := make(chan streamChunk, 64)
	go func() {
		defer close(chunks)
		defer resp.Body.Close()
		sc := newScanner(resp.Body)
		for {
			raw, done, err := sc.Next()
			if err != nil {
				// Surface as a synthetic error chunk? Cleaner: return via
				// out channel by injecting a zero chunk with no choices
				// then have translate emit MessageStop. Simpler: just stop.
				return
			}
			if done {
				return
			}
			var ch streamChunk
			if err := json.Unmarshal(raw, &ch); err != nil {
				// Malformed frame — skip and continue (logged at debug elsewhere).
				continue
			}
			select {
			case chunks <- ch:
			case <-ctx.Done():
				return
			}
		}
	}()

	var parser *Parser
	if c.parseThinkTags {
		parser = NewThinkTagParser()
	}
	return translate(chunks, caps, parser), nil
}

// decodeErrorResponse reads up to 64 KiB of the response body, attempts to
// decode the OpenAI error envelope, and returns an EventError wrapping a
// descriptive message (including Retry-After on 429 where available).
func decodeErrorResponse(resp *http.Response) llm.StreamEvent {
	const cap = 64 * 1024
	body, _ := io.ReadAll(io.LimitReader(resp.Body, cap))
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	var msg, typ string
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		msg, typ = env.Error.Message, env.Error.Type
	} else {
		msg = strings.TrimSpace(string(body))
	}
	err := fmt.Errorf("openai %d %s: %s", resp.StatusCode, typ, msg)
	if resp.StatusCode == 429 {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			err = fmt.Errorf("openai 429 %s: %s (retry-after %s)", typ, msg, ra)
		}
	}
	return llm.StreamEvent{Type: llm.EventError, Err: err}
}
