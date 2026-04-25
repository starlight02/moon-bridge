package app

import (
	"bytes"
	"testing"
)

func TestWelcomeMessage(t *testing.T) {
	want := "Welcome to Moon Bridge!"

	if got := WelcomeMessage(); got != want {
		t.Fatalf("WelcomeMessage() = %q, want %q", got, want)
	}
}

func TestRunWritesWelcomeMessage(t *testing.T) {
	var output bytes.Buffer

	Run(&output)

	want := "Welcome to Moon Bridge!\n"
	if got := output.String(); got != want {
		t.Fatalf("Run() wrote %q, want %q", got, want)
	}
}
