package provider

import (
	"context"
	"os"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
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

// Create is the "add Provider" path and must refuse an ID that is taken. Save
// stays an upsert, because editing a Provider legitimately overwrites it, and
// SaveKey rewrites the whole entry to rotate a key.
func TestCreateRefusesAnIDThatAlreadyExists(t *testing.T) {
	store := NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
	first := Entry{ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test/openai", APIKey: "sk-first"}
	if _, err := store.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := Entry{ID: "acme", Name: "Impostor", BaseURL: "https://api.impostor.test/openai", APIKey: "sk-second"}
	if _, err := store.Create(context.Background(), second); err == nil {
		t.Fatal("creating a Provider with a taken ID succeeded")
	} else if oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("duplicate ID error code = %v", oneerrors.As(err).Code)
	}
	// The point of refusing is that the original survives untouched.
	got, err := store.Get("acme")
	if err != nil || got.Name != "Acme" || got.APIKey != "sk-first" {
		t.Fatalf("refused create still modified the Provider: %#v, err=%v", got, err)
	}
	// Editing the same ID through Save is still allowed.
	if _, err := store.Save(context.Background(), second); err != nil {
		t.Fatalf("editing an existing Provider was refused: %v", err)
	}
}

// A built-in ID is reserved too. Shadowing one used to be silent, and because
// SaveProvider reapplies the entry to every Agent bound to that ID, it repointed
// working Agents at another vendor's endpoint.
func TestCreateRefusesABuiltInProviderID(t *testing.T) {
	store := NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
	_, err := store.Create(context.Background(), Entry{
		ID: "ppio", Name: "Not PPIO", BaseURL: "https://api.impostor.test/openai",
	})
	if err == nil {
		t.Fatal("creating a Provider over a built-in ID succeeded")
	}
	if oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("built-in collision error code = %v", oneerrors.As(err).Code)
	}
	// Overriding a built-in endpoint remains possible through the edit path.
	if _, err := store.Save(context.Background(), Entry{
		ID: "ppio", Name: "PPIO", BaseURL: "https://relay.ppio.test/openai",
	}); err != nil {
		t.Fatalf("editing a built-in Provider was refused: %v", err)
	}
}
