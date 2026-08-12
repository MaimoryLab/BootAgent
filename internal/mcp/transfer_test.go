package mcp

import (
	"strings"
	"testing"
)

func transferRegistry() Registry {
	return Registry{SchemaVersion: RegistrySchemaVersion, Servers: map[string]ServerFact{
		"demo": {Variants: []Variant{{Agents: []string{"codex"}, Spec: Spec{Type: "stdio", Command: "demo", Env: map[string]string{"TOKEN": "secret"}}}}},
	}}
}

func TestTransferOmitRetainsSecretPaths(t *testing.T) {
	b, err := Export(transferRegistry(), ExportOptions{Mode: SecretOmit})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"TOKEN":"secret"`) || !strings.Contains(string(b), "TOKEN") {
		t.Fatalf("unexpected omit payload: %s", b)
	}
	r, err := Import(b, "")
	if err != nil || r.Servers["demo"].Variants[0].Spec.Env["TOKEN"] != RedactedValue {
		t.Fatalf("omit import: %#v, %v", r, err)
	}
}

func TestTransferEncryptedRoundTrip(t *testing.T) {
	b, err := Export(transferRegistry(), ExportOptions{Mode: SecretEncrypted, Password: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"TOKEN":"secret"`) {
		t.Fatal("secret leaked in encrypted envelope")
	}
	r, err := Import(b, "pass")
	if err != nil || r.Servers["demo"].Variants[0].Spec.Env["TOKEN"] != "secret" {
		t.Fatalf("encrypted import: %#v, %v", r, err)
	}
}

func TestTransferPlaintextRequiresConfirmation(t *testing.T) {
	if _, err := Export(transferRegistry(), ExportOptions{Mode: SecretPlaintext}); err == nil {
		t.Fatal("plaintext export without confirmation")
	}
	b, err := Export(transferRegistry(), ExportOptions{Mode: SecretPlaintext, ConfirmPlaintext: true})
	if err != nil || !strings.Contains(string(b), "secret") {
		t.Fatalf("plaintext export: %v", err)
	}
}

func TestTransferRejectsDuplicateAndInvalidEnvelope(t *testing.T) {
	if _, err := Import([]byte(`{"schema_version":99}`), ""); err == nil {
		t.Fatal("newer envelope accepted")
	}
	if _, err := Export(Registry{SchemaVersion: RegistrySchemaVersion, Servers: map[string]ServerFact{"bad id": {}}}, ExportOptions{Mode: SecretOmit}); err == nil {
		t.Fatal("invalid ID accepted")
	}
}

func TestTransferMCPSelectionOmitsAgentBindings(t *testing.T) {
	b, err := Export(transferRegistry(), ExportOptions{Mode: SecretOmit, ServerIDs: []string{"demo"}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Import(b, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Servers["demo"].Variants[0].Agents; len(got) != 0 {
		t.Fatalf("agents leaked into transfer: %#v", got)
	}
}
