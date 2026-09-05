package settings

import (
	"path/filepath"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	user := Settings{DefaultModel: &ModelRef{Provider: "u", ID: "u"}}
	project := Settings{DefaultModel: &ModelRef{Provider: "p", ID: "p"}}
	if got := Resolve(nil, project, user); got.ID != "p" {
		t.Fatalf("project=%#v", got)
	}
	cli := &ModelRef{Provider: "c", ID: "c"}
	if got := Resolve(cli, project, user); got.ID != "c" {
		t.Fatalf("cli=%#v", got)
	}
}

func TestPiCompatiblePaths(t *testing.T) {
	if got := UserPath("C:/home"); got != filepath.Join("C:/home", ".pi", "agent", "settings.json") {
		t.Fatalf("user path=%q", got)
	}
	if got := ProjectPath("C:/repo"); got != filepath.Join("C:/repo", ".pi", "settings.json") {
		t.Fatalf("project path=%q", got)
	}
}
