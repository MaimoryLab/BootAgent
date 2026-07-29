package provider

import (
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
)

func TestEveryProbeableProtocolHasALabel(t *testing.T) {
	// The label appears in the probe result the user reads, so a protocol
	// without one would report a blank explanation of what was tested.
	for _, protocol := range []string{catalog.ProtocolOpenAI, catalog.ProtocolAnthropic, catalog.ProtocolResponses} {
		if !KnownProtocol(protocol) {
			t.Errorf("%s is not probeable", protocol)
		}
		if ProtocolLabel(protocol) == protocol {
			t.Errorf("%s has no human-readable label", protocol)
		}
	}
}

func TestAnUnknownProtocolIsVisibleRatherThanBlank(t *testing.T) {
	if got := ProtocolLabel("carrier-pigeon"); got != "carrier-pigeon" {
		t.Errorf("label = %q, want the raw id so it is visible", got)
	}
	if KnownProtocol("carrier-pigeon") {
		t.Error("an unrecognised protocol must not be reported as probeable")
	}
}

func TestEveryAutoAgentsProtocolIsProbeable(t *testing.T) {
	// A manifest entry naming an adapter whose protocol OneAgent cannot probe
	// would pass configuration and then fail the connection test for a reason
	// the user cannot act on.
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		protocol := catalog.AgentProtocol(agent.ConfigAdapter)
		if !KnownProtocol(protocol) {
			t.Errorf("%s speaks %q, which cannot be probed", id, protocol)
		}
	}
}

func TestARouteThatIsAbsentMeansThePairCanNeverWork(t *testing.T) {
	// Permanent, so it must not be reported as retryable: retrying cannot help,
	// choosing a different model can.
	for _, code := range []int{404, 405, 501} {
		if !UnsupportedProtocol(code, "") {
			t.Errorf("HTTP %d should classify as unsupported", code)
		}
	}
}

func TestAnErrorBodySayingSoAlsoMeansUnsupported(t *testing.T) {
	// Endpoints answer the same fact with a 400, a 422 or a 500 plus a message.
	for _, testCase := range []struct {
		code int
		body string
	}{
		{400, `{"error":{"message":"This model does not support endpoint /v1/responses"}}`},
		{422, "not implemented for this model"},
		{500, "Unsupported endpoint"},
		{400, "UNKNOWN ENDPOINT"},
	} {
		if !UnsupportedProtocol(testCase.code, testCase.body) {
			t.Errorf("HTTP %d with body %q should classify as unsupported", testCase.code, testCase.body)
		}
	}
}

func TestAnOrdinaryFailureIsNotMistakenForUnsupported(t *testing.T) {
	// Misclassifying these would tell the user to change model when the real
	// problem is the key, the quota or the endpoint being down.
	for _, testCase := range []struct {
		code int
		body string
	}{
		{401, "invalid api key"},
		{403, "forbidden"},
		{429, "rate limited"},
		{500, "internal server error"},
		{400, "missing required field: model"},
		{503, "does not support endpoint"}, // right words, wrong status
	} {
		if UnsupportedProtocol(testCase.code, testCase.body) {
			t.Errorf("HTTP %d with body %q must not classify as unsupported", testCase.code, testCase.body)
		}
	}
}

func TestTheMarkerSearchIsCaseInsensitive(t *testing.T) {
	if !UnsupportedProtocol(400, "Does Not Support Endpoint") {
		t.Error("marker matching should ignore case")
	}
}

func TestPickChatModelSkipsModelsThatPlainlyCannotChat(t *testing.T) {
	// Probing an embedding model fails for a reason unrelated to connectivity
	// or the key, which sends the user looking for the wrong problem.
	models := []string{
		"whisper-large-v3",
		"bge-reranker-v2",
		"text-embeddings-large",
		"qwen-vl-max",
		"deepseek-v3",
		"gpt-5.6-terra",
	}
	if got := PickChatModel(models); got != "deepseek-v3" {
		t.Fatalf("picked %q, want deepseek-v3", got)
	}
}

func TestMarkersMatchOnSeparatorsSoOrdinaryNamesStayEligible(t *testing.T) {
	// "resolver" contains "sql" backwards-adjacent and "evolve" contains "vl".
	// Matching bare substrings would reject both, and a provider whose whole
	// catalog was rejected would get an arbitrary first entry instead.
	if got := PickChatModel([]string{"resolver-1", "evolve-chat"}); got != "resolver-1" {
		t.Errorf("picked %q, want resolver-1", got)
	}
	for _, model := range []string{"resolver-1", "evolve-chat", "visionary-max", "flexible-1"} {
		if nonChatModel.MatchString(model) {
			t.Errorf("%q was classified as non-chat", model)
		}
	}
}

func TestWhenEveryModelLooksNonChatTheFirstIsStillReturned(t *testing.T) {
	// The classifier is a heuristic and may be wrong. Returning nothing would
	// block the wizard; probing the first entry lets the failure explain itself.
	got := PickChatModel([]string{"text-embedding-3-small", "whisper-1"})
	if got != "text-embedding-3-small" {
		t.Fatalf("picked %q, want the first entry", got)
	}
}

func TestAnEmptyListYieldsAnEmptyModelRatherThanPanicking(t *testing.T) {
	// Callers only reach this after a successful discovery, but a provider that
	// answers with an empty array should not crash the probe.
	if got := PickChatModel(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
