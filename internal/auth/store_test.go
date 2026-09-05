package auth

import "testing"

func TestCredentialStoreRoundTrip(t *testing.T) {
	path := t.TempDir() + "/auth.json"
	file := File{}
	file.Set("openai", "secret")
	if err := Save(path, file); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Get("openai") != "secret" {
		t.Fatalf("%#v %v", got, err)
	}
	got.Delete("openai")
	if got.Get("openai") != "" {
		t.Fatal("delete failed")
	}
}
