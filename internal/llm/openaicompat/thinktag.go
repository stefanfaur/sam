package openaicompat

import "bytes"

// State represents the think-tag scanner's position relative to tag boundaries.
type tagState int

const (
	stateOutside    tagState = iota // plain text
	stateMaybeOpen                  // consumed a prefix of `<think>`
	stateInside                     // between `<think>` and `</think>`
	stateMaybeClose                 // consumed a prefix of `</think>`
)

var (
	openTag  = []byte("<think>")
	closeTag = []byte("</think>")
)

// Parser is a streaming <think>...</think> extractor with byte-level
// peek-then-append semantics. A Parser instance is stateful between Feed
// calls; callers pass text deltas in order and call Flush() at stream end.
type Parser struct {
	state tagState
	carry []byte
}

// NewThinkTagParser returns a parser in the OUTSIDE state.
func NewThinkTagParser() *Parser {
	return &Parser{state: stateOutside}
}

// Feed processes a chunk of streaming text and returns the portion that was
// classified during this call. Unclassified bytes (partial tag matches) stay
// in the parser's carry for the next Feed or Flush.
func (p *Parser) Feed(chunk string) (text, thinking string) {
	var tb, kb []byte
	for i := 0; i < len(chunk); i++ {
		p.step(chunk[i], &tb, &kb)
	}
	return string(tb), string(kb)
}

// Flush drains any pending carry at end of stream. Partial open-tag matches
// become text; partial close-tag matches become thinking. Inside-without-close
// text was already emitted during Feed, so Flush returns empty in that case.
func (p *Parser) Flush() (text, thinking string) {
	switch p.state {
	case stateMaybeOpen:
		text = string(p.carry)
		p.carry = nil
		p.state = stateOutside
	case stateMaybeClose:
		thinking = string(p.carry)
		p.carry = nil
		p.state = stateInside
	}
	return text, thinking
}

func (p *Parser) step(b byte, tb, kb *[]byte) {
	// A divergence may fall through to re-process the same byte in the new
	// state; bounded to one extra iteration per divergence per state change.
	for {
		switch p.state {
		case stateOutside:
			if b == '<' {
				p.state = stateMaybeOpen
				p.carry = []byte{'<'}
				return
			}
			*tb = append(*tb, b)
			return

		case stateMaybeOpen:
			candidate := append(append([]byte(nil), p.carry...), b)
			if bytes.Equal(candidate, openTag) {
				p.carry = nil
				p.state = stateInside
				return
			}
			if bytes.HasPrefix(openTag, candidate) {
				p.carry = candidate
				return
			}
			// Divergence: flush carry as text, drop to OUTSIDE, reprocess b.
			*tb = append(*tb, p.carry...)
			p.carry = nil
			p.state = stateOutside
			// fall through — reprocess b

		case stateInside:
			if b == '<' {
				p.state = stateMaybeClose
				p.carry = []byte{'<'}
				return
			}
			*kb = append(*kb, b)
			return

		case stateMaybeClose:
			candidate := append(append([]byte(nil), p.carry...), b)
			if bytes.Equal(candidate, closeTag) {
				p.carry = nil
				p.state = stateOutside
				return
			}
			if bytes.HasPrefix(closeTag, candidate) {
				p.carry = candidate
				return
			}
			// Divergence: flush carry as thinking, back to INSIDE, reprocess b.
			*kb = append(*kb, p.carry...)
			p.carry = nil
			p.state = stateInside
			// fall through — reprocess b
		}
	}
}
