package cli

import (
	"strings"
	"testing"
)

func TestReadMessageInput_Explicit(t *testing.T) {
	got, err := readMessageInput("hello", false, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "hello" {
		t.Fatalf("want hello, got %q", got)
	}
}

func TestReadMessageInput_Stdin(t *testing.T) {
	got, err := readMessageInput("", true, strings.NewReader("from stdin\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "from stdin" {
		t.Fatalf("want %q, got %q", "from stdin", got)
	}
}

func TestReadMessageInput_StdinEmpty(t *testing.T) {
	_, err := readMessageInput("", true, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error on empty stdin")
	}
}

func TestReadMessageInput_None(t *testing.T) {
	_, err := readMessageInput("", false, nil)
	if err == nil {
		t.Fatal("expected error when no message provided")
	}
}

func TestNormalizeEmoji(t *testing.T) {
	cases := map[string]string{
		":fire:":     "fire",
		"fire":       "fire",
		":+1:":       "+1",
		"  :wave:  ": "wave",
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeEmoji(in); got != want {
			t.Fatalf("normalizeEmoji(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidStatuses(t *testing.T) {
	for _, s := range []string{"online", "away", "dnd", "offline"} {
		if !validStatuses[s] {
			t.Fatalf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "busy", "ooo", "Online"} {
		if validStatuses[s] {
			t.Fatalf("did not expect %q to be valid", s)
		}
	}
}
