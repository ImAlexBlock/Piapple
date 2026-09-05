package settings

import "testing"

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
