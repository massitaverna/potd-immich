package blocklist

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestPrompterAnswers(t *testing.T) {
	cases := map[string]bool{
		"y\n":     true,
		"Y\n":     true,
		"yes\n":   true,
		" YES \n": true,
		"y":       true,
		"\n":      false,
		"n\n":     false,
		"no\n":    false,
		"NO\n":    false,
	}
	for input, want := range cases {
		var out strings.Builder
		got, err := Prompter(strings.NewReader(input), &out)("Anna")
		if err != nil || got != want {
			t.Errorf("input %q: got %v, %v; want %v", input, got, err, want)
		}
		if out.String() != "Add Anna to blocklist? (y/N): " {
			t.Errorf("prompt = %q", out.String())
		}
	}
}

func TestPrompterReadsOneLinePerQuestion(t *testing.T) {
	var out strings.Builder
	confirm := Prompter(strings.NewReader("y\n\nyes\n"), &out)
	var got []bool
	for _, name := range []string{"Anna", "Bea", "Carl"} {
		yes, err := confirm(name)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, yes)
	}
	if !slices.Equal(got, []bool{true, false, true}) {
		t.Errorf("answers = %v, want [true false true]", got)
	}
	want := "Add Anna to blocklist? (y/N): Add Bea to blocklist? (y/N): Add Carl to blocklist? (y/N): "
	if out.String() != want {
		t.Errorf("prompts = %q", out.String())
	}
}

func TestPrompterReasksOnInvalidAnswer(t *testing.T) {
	const (
		prompt = "Add Anna to blocklist? (y/N): "
		hint   = "Please answer y or n.\n"
	)
	cases := []struct {
		input string
		want  bool
		out   string
	}{
		{"yep\ny\n", true, prompt + hint + prompt},
		{"maybe\nnope\nn\n", false, prompt + hint + prompt + hint + prompt},
		{"x\n\n", false, prompt + hint + prompt},
	}
	for _, c := range cases {
		var out strings.Builder
		got, err := Prompter(strings.NewReader(c.input), &out)("Anna")
		if err != nil || got != c.want {
			t.Errorf("input %q: got %v, %v; want %v", c.input, got, err, c.want)
		}
		if out.String() != c.out {
			t.Errorf("input %q: output %q, want %q", c.input, out.String(), c.out)
		}
	}
}

func TestPrompterInvalidAnswerThenNoInput(t *testing.T) {
	var out strings.Builder
	if _, err := Prompter(strings.NewReader("yep\n"), &out)("Anna"); !errors.Is(err, errNoAnswer) {
		t.Errorf("err = %v, want errNoAnswer", err)
	}
}

func TestPrompterNoInput(t *testing.T) {
	var out strings.Builder
	if _, err := Prompter(strings.NewReader(""), &out)("Anna"); !errors.Is(err, errNoAnswer) {
		t.Errorf("err = %v, want errNoAnswer", err)
	}
}
