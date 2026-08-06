package provider

import (
	"context"
	"os"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func TestStorePersistsCRUDAndPrivateKey(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home, securefs.New(securefs.Options{OS: "linux"}))
	want := Entry{
		ID: "acme", Name: "Acme", Home: "https://acme.test/",
		BaseURL: "https://api.acme.test/openai", AnthropicBaseURL: "https://api.acme.test/anthropic",
		APIKey: "sk-persisted",
	}
	if _, err := store.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("provider file mode = %v", info.Mode().Perm())
	}

	reloaded := NewStore(home, securefs.New(securefs.Options{OS: "linux"}))
	got, err := reloaded.Get("acme")
	if err != nil || got.APIKey != want.APIKey || got.BaseURL != want.BaseURL || got.BuiltIn {
		t.Fatalf("reloaded Provider = %#v, err=%v", got, err)
	}
	public, err := reloaded.Public()
	if err != nil || !public["acme"].Custom || !public["acme"].HasKey {
		t.Fatalf("public Providers = %#v, err=%v", public, err)
	}
	if err := reloaded.Delete(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.Get("acme"); err == nil {
		t.Fatal("deleted Provider was still readable")
	}
}

func TestStoreAcceptsEitherAPIEndpoint(t *testing.T) {
	store := NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
	for _, entry := range []Entry{
		{ID: "anthropic-only", Name: "Anthropic", AnthropicBaseURL: "https://api.example.test/anthropic"},
		{ID: "openai-only", Name: "OpenAI", BaseURL: "https://api.example.test/openai"},
	} {
		if _, err := store.Save(context.Background(), entry); err != nil {
			t.Fatalf("save %s: %v", entry.ID, err)
		}
	}
	if _, err := store.Save(context.Background(), Entry{ID: "empty", Name: "Empty"}); err == nil {
		t.Fatal("empty Provider unexpectedly saved")
	}
}
