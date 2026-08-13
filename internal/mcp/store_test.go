package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func TestStoreLoadAbsentAndRoundTrip(t *testing.T) {
	home := t.TempDir()
	fs := securefs.New(securefs.Options{OS: "linux"})
	s := NewStore(home, fs)
	r, err := s.Load()
	if err != nil || r.SchemaVersion != RegistrySchemaVersion || len(r.Servers) != 0 {
		t.Fatalf("empty registry: %#v, %v", r, err)
	}
	r.Servers["demo"] = ServerFact{Variants: []Variant{{Agents: []string{"codex"}, Spec: Spec{Command: "npx", Args: []string{"demo"}}}}}
	if err := s.Save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil || !EqualNormalized(loaded.Servers["demo"].Variants[0].Spec, r.Servers["demo"].Variants[0].Spec) {
		t.Fatalf("round trip: %#v, %v", loaded, err)
	}
	info, err := os.Stat(s.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode: %v, %v", info.Mode().Perm(), err)
	}
}

func TestStoreRejectsMalformedAndNewerWithoutOverwrite(t *testing.T) {
	home := t.TempDir()
	fs := securefs.New(securefs.Options{OS: "linux"})
	s := NewStore(home, fs)
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"{", `{"schema_version":99,"servers":{}}`} {
		if err := os.WriteFile(s.Path(), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(); err == nil {
			t.Fatalf("accepted %q", data)
		}
		got, _ := os.ReadFile(s.Path())
		if string(got) != data {
			t.Fatal("invalid registry was overwritten")
		}
	}
}

func TestStoreRejectsInvalidServerIDs(t *testing.T) {
	s := NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
	r := Registry{SchemaVersion: RegistrySchemaVersion, Servers: map[string]ServerFact{"Bad ID": {}}}
	if err := s.Save(context.Background(), r); err == nil || !strings.Contains(err.Error(), "server ID") {
		t.Fatalf("expected ID error, got %v", err)
	}
}
