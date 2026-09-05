package main

import "testing"

func TestNormalizeLongOptions(t *testing.T) {
	got := normalizeLongOptions([]string{"--model", "gpt-4o", "--json", "--", "--literal"})
	want := []string{"-model", "gpt-4o", "-json", "--", "-literal"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%#v want=%#v", got, want)
		}
	}
}
