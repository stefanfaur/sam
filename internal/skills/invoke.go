package skills

import "strings"

// RenderInvocation returns the skill body with $ARGUMENTS substituted. A
// literal `\$ARGUMENTS` in the body is left as the string `$ARGUMENTS`.
//
// If the caller passes non-empty args but the body never references
// $ARGUMENTS, the args are appended to the rendered body as a trailing
// "Additional user input" block so users aren't surprised when words after
// the slash command vanish into the void.
//
// Returns ErrBodyTooLarge if the rendered body exceeds MaxBodyBytes.
func RenderInvocation(sk *Skill, args string) (string, error) {
	args = strings.TrimSpace(args)
	body := sk.Body

	const sentinel = "\x00SKILL_ARGS_ESCAPE\x00"
	referenced := strings.Contains(body, `$ARGUMENTS`) && !isOnlyEscaped(body)
	body = strings.ReplaceAll(body, `\$ARGUMENTS`, sentinel)
	body = strings.ReplaceAll(body, `$ARGUMENTS`, args)
	body = strings.ReplaceAll(body, sentinel, `$ARGUMENTS`)

	if args != "" && !referenced {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += "\n---\nAdditional user input: " + args + "\n"
	}

	if len(body) > MaxBodyBytes {
		return "", ErrBodyTooLarge
	}
	return body, nil
}

// isOnlyEscaped reports whether every $ARGUMENTS occurrence in body is
// preceded by `\` (i.e. all escaped). Such a body is treated as not
// referencing $ARGUMENTS so trailing user args still get appended.
func isOnlyEscaped(body string) bool {
	i := 0
	for {
		j := strings.Index(body[i:], `$ARGUMENTS`)
		if j < 0 {
			return true
		}
		abs := i + j
		if abs == 0 || body[abs-1] != '\\' {
			return false
		}
		i = abs + len(`$ARGUMENTS`)
	}
}

// ErrBodyTooLarge is returned when the rendered body exceeds the size cap.
var ErrBodyTooLarge = constError("skills: rendered body exceeds 50 KB")

type constError string

func (e constError) Error() string { return string(e) }
