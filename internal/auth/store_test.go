package auth

import (
	"path/filepath"
	"testing"
)

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

func TestPiCompatibleAuthPath(t *testing.T) {
	if got := Path("C:/home"); got != filepath.Join("C:/home", ".pi", "agent", "auth.json") {
		t.Fatalf("path=%q", got)
	}
}

func TestCredentialStoreNormalizesProviderAliases(t *testing.T) {
	file := File{}
	file.Set("gemini", "google-key")
	if file.Get("google") != "google-key" || file.Get("gemini") != "google-key" {
		t.Fatalf("credentials=%#v", file)
	}
	file.Delete("gemini")
	if file.Get("google") != "" {
		t.Fatal("alias delete failed")
	}
}
