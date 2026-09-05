package main

import (
	"strings"
	"testing"
)

func TestNormalizeLongOptions(t *testing.T) {
	got := normalizeLongOptions([]string{"--model", "gpt-4o", "--json", "--", "--literal"})
	want := []string{"-model", "gpt-4o", "-json", "--", "-literal"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%#v want=%#v", got, want)
		}
	}
}

func TestQualifiedModelCanBeParsed(t *testing.T) {
	parts := strings.SplitN("openrouter/anthropic/claude-sonnet-4", "/", 2)
	if parts[0] != "openrouter" || parts[1] != "anthropic/claude-sonnet-4" {
		t.Fatalf("parts=%#v", parts)
	}
}
