package skills

import (
	"strings"
	"testing"
)

func TestRenderInvocation(t *testing.T) {
	// These cases cover the substitution + escape grammar. They assume the
	// body references $ARGUMENTS (at least one unescaped occurrence) OR args
	// is empty — otherwise the trailing "Additional user input" block is
	// appended and asserted elsewhere.
	cases := []struct {
		name string
		body string
		args string
		want string
	}{
		{"no-args", "Hello world", "", "Hello world"},
		{"single-sub", "Review PR $ARGUMENTS now", "123", "Review PR 123 now"},
		{"multi-sub", "$ARGUMENTS and $ARGUMENTS again", "x", "x and x again"},
		{"empty-sub", "Before $ARGUMENTS after", "", "Before  after"},
		{"trim-args", "A $ARGUMENTS Z", "  foo bar  ", "A foo bar Z"},
		{"escape-then-sub", `A \$ARGUMENTS and $ARGUMENTS`, "x", "A $ARGUMENTS and x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sk := &Skill{Body: tc.body}
			got, err := RenderInvocation(sk, tc.args)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRenderInvocation_AppendsWhenBodyIgnoresArgs(t *testing.T) {
	sk := &Skill{Body: "Interview the user about the attached plan."}
	got, err := RenderInvocation(sk, "build an openai provider")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Interview the user") {
		t.Errorf("body missing: %q", got)
	}
	if !strings.Contains(got, "Additional user input: build an openai provider") {
		t.Errorf("args not appended: %q", got)
	}
}

func TestRenderInvocation_NoAppendWhenArgsEmpty(t *testing.T) {
	sk := &Skill{Body: "Body without placeholder."}
	got, _ := RenderInvocation(sk, "")
	if strings.Contains(got, "Additional user input") {
		t.Errorf("should not append block when args empty: %q", got)
	}
}

func TestRenderInvocation_NoAppendWhenBodyReferencedArgs(t *testing.T) {
	sk := &Skill{Body: "Review PR #$ARGUMENTS carefully."}
	got, _ := RenderInvocation(sk, "123")
	if strings.Contains(got, "Additional user input") {
		t.Errorf("should not append when body already substituted: %q", got)
	}
	if !strings.Contains(got, "Review PR #123") {
		t.Errorf("substitution missing: %q", got)
	}
}

func TestRenderInvocation_AppendsWhenAllArgsEscaped(t *testing.T) {
	sk := &Skill{Body: `Literal \$ARGUMENTS in docs.`}
	got, _ := RenderInvocation(sk, "real args")
	if !strings.Contains(got, "Additional user input: real args") {
		t.Errorf("should append when all $ARGUMENTS escaped: %q", got)
	}
	if !strings.Contains(got, "Literal $ARGUMENTS in docs") {
		t.Errorf("escape should still produce literal: %q", got)
	}
}

func TestRenderInvocation_TooLargeAfterSub(t *testing.T) {
	big := strings.Repeat("x", MaxBodyBytes-5)
	sk := &Skill{Body: "$ARGUMENTS" + big}
	// Substitute with 100 chars -> final size > MaxBodyBytes.
	_, err := RenderInvocation(sk, strings.Repeat("y", 100))
	if err != ErrBodyTooLarge {
		t.Errorf("err = %v, want ErrBodyTooLarge", err)
	}
}
