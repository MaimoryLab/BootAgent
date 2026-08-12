package mcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestValidateID(t *testing.T) {
	for _, id := range []string{"context7", "team.one", "a_b-2"} {
		if err := ValidateID(id); err != nil {
			t.Fatalf("ValidateID(%q): %v", id, err)
		}
	}
	for _, id := range []string{"", "-bad", "has space", "a/b"} {
		if err := ValidateID(id); err == nil {
			t.Fatalf("ValidateID(%q) accepted", id)
		}
	}
}

func TestNormalizeAndEqual(t *testing.T) {
	a := Spec{Command: " npx ", Args: []string{"-y", "x"}, Env: map[string]string{"B": "2", "A": "1"}, Extensions: map[string]json.RawMessage{"x": json.RawMessage(`{"b":2,"a":1}`)}}
	b := Spec{Type: "stdio", Command: "npx", Args: []string{"-y", "x"}, Env: map[string]string{"A": "1", "B": "2"}, Extensions: map[string]json.RawMessage{"x": json.RawMessage(`{"a":1,"b":2}`)}}
	if !EqualNormalized(a, b) {
		t.Fatal("equivalent specs differ")
	}
	b.Args[0] = "x"
	if EqualNormalized(a, b) {
		t.Fatal("ordered args were ignored")
	}
}

func TestNormalizeDropsIncompatibleTransportFields(t *testing.T) {
	remote, err := Normalize(Spec{Type: "http", URL: "https://example.test", Command: "npx", Args: []string{"server"}, Cwd: "/tmp", Env: map[string]string{"TOKEN": "secret"}, Headers: map[string]string{"Authorization": "secret"}})
	if err != nil { t.Fatal(err) }
	if remote.Command != "" || len(remote.Args) != 0 || remote.Cwd != "" || len(remote.Env) != 0 { t.Fatalf("stdio fields retained: %#v", remote) }
	stdio, err := Normalize(Spec{Type: "stdio", Command: "npx", URL: "https://example.test", Headers: map[string]string{"Authorization": "secret"}})
	if err != nil { t.Fatal(err) }
	if stdio.URL != "" || len(stdio.Headers) != 0 { t.Fatalf("remote fields retained: %#v", stdio) }
}

func TestRedactSpec(t *testing.T) {
	s := Spec{Command: "server", Env: map[string]string{"TOKEN": "secret", "MODE": "dev"}, Headers: map[string]string{"Authorization": "Bearer secret"}, Extensions: map[string]json.RawMessage{"nested": json.RawMessage(`{"token":"deep","keep":true}`)}}
	redacted, paths := RedactSpec(s)
	if redacted.Env["TOKEN"] != RedactedValue || redacted.Headers["Authorization"] != RedactedValue {
		t.Fatalf("direct secrets not redacted: %#v", redacted)
	}
	if string(redacted.Extensions["nested"]) == string(s.Extensions["nested"]) {
		t.Fatal("nested secret not redacted")
	}
	if len(paths) < 3 || !reflect.DeepEqual(s.Env["TOKEN"], "secret") {
		t.Fatalf("unexpected secret paths: %#v", paths)
	}
}
