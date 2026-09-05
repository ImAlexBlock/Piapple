package models

import "testing"

func TestParseRef(t *testing.T) {
	provider, id, err := ParseRef(" Anthropic/claude-sonnet-4-5 ")
	if err != nil || provider != "anthropic" || id != "claude-sonnet-4-5" {
		t.Fatalf("%q %q %v", provider, id, err)
	}
}
func TestParseRefRejectsMissingParts(t *testing.T) {
	for _, value := range []string{"", "gpt-4o", "/gpt-4o", "openai/"} {
		if _, _, err := ParseRef(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
func TestFindAllowsCompatibleUnlistedModels(t *testing.T) {
	m, ok := Find("openai", "my-compatible-model")
	if ok || m.Provider != "openai" || m.ID != "my-compatible-model" {
		t.Fatalf("%#v %v", m, ok)
	}
}
