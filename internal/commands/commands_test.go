package commands

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	c, ok := Parse("/model anthropic/claude")
	if !ok || c.Name != "model" || c.Arguments != "anthropic/claude" {
		t.Fatalf("%#v %v", c, ok)
	}
}
func TestHelpContainsPiCommandSet(t *testing.T) {
	for _, want := range []string{"/login", "/model", "/new", "/compact", "/resume", "/tree"} {
		if !strings.Contains(Help(), want) {
			t.Fatalf("missing %s", want)
		}
	}
}
