package install

import (
	"strings"
	"testing"
)

func TestEverySecretIsReplacedEverywhereItAppears(t *testing.T) {
	// npm can echo the same variable more than once in one failure.
	got := Redact("key=sk-a and again sk-a plus sk-b", []string{"sk-a", "sk-b"})
	if strings.Contains(got, "sk-a") || strings.Contains(got, "sk-b") {
		t.Fatalf("got %q, want every occurrence replaced", got)
	}
	if strings.Count(got, "[redacted]") != 3 {
		t.Errorf("got %q, want three replacements", got)
	}
}

func TestAnEmptySecretIsSkippedRatherThanMatchingEverywhere(t *testing.T) {
	// Replacing "" would insert the placeholder between every character, which
	// destroys the message while proving nothing.
	got := Redact("nothing to hide", []string{""})
	if got != "nothing to hide" {
		t.Fatalf("got %q, want the text unchanged", got)
	}
}

func TestOnlyCredentialLookingVariablesAreTreatedAsSecrets(t *testing.T) {
	// Redacting everything would remove the detail that makes a failure
	// diagnosable; redacting too little leaks the key.
	secrets := SecretsIn(map[string]string{
		"ONEAGENT_API_KEY_CODEX": "sk-key",
		"ANTHROPIC_AUTH_TOKEN":   "sk-token",
		"MY_SECRET":              "sk-secret",
		"DB_PASSWORD":            "hunter2",
		"lowercase_key":          "sk-lower",
		"PATH":                   "/usr/bin",
		"HOME":                   "/home/user",
		"NPM_CONFIG_REGISTRY":    "https://registry.npmjs.org/",
	})
	found := map[string]bool{}
	for _, value := range secrets {
		found[value] = true
	}
	for _, want := range []string{"sk-key", "sk-token", "sk-secret", "hunter2", "sk-lower"} {
		if !found[want] {
			t.Errorf("%q was not treated as a secret", want)
		}
	}
	for _, unwanted := range []string{"/usr/bin", "/home/user", "https://registry.npmjs.org/"} {
		if found[unwanted] {
			t.Errorf("%q was treated as a secret", unwanted)
		}
	}
}

func TestAnEmptyValuedVariableIsNotCollectedAsASecret(t *testing.T) {
	// It would become an empty search string, which matches everywhere.
	if got := SecretsIn(map[string]string{"API_KEY": ""}); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

func TestTheFailureSummaryKeepsTheEndWhereTheCauseIs(t *testing.T) {
	// npm puts hundreds of lines of preamble before the actual error.
	stderr := strings.Repeat("npm WARN noise\n", 50) + "npm ERR! code E404\nnpm ERR! 404 Not Found\n"
	got := FailureDetail(map[string]string{}, "", stderr)
	if !strings.Contains(got, "404 Not Found") {
		t.Errorf("summary = %q, want the last lines", got)
	}
	if strings.Count(got, "|") > 2 {
		t.Errorf("summary = %q, want at most three lines", got)
	}
}

func TestTheSummaryIsRedactedBeforeItIsTruncated(t *testing.T) {
	// The other order could cut a key in half and leave the first part visible.
	const secret = "sk-0123456789abcdef"
	long := strings.Repeat("x", 590) + "\nkey=" + secret
	got := FailureDetail(map[string]string{"API_KEY": secret}, "", long)
	if strings.Contains(got, "sk-0123") {
		t.Errorf("summary = %q, want no fragment of the key", got)
	}
	if len(got) > failureDetailLimit {
		t.Errorf("summary is %d bytes, want at most %d", len(got), failureDetailLimit)
	}
}

func TestColourCodesAreStrippedSoTheMessageIsReadable(t *testing.T) {
	got := FailureDetail(map[string]string{}, "", "\x1b[31mnpm ERR! failed\x1b[0m")
	if strings.Contains(got, "\x1b") {
		t.Errorf("summary = %q, want no escape sequences", got)
	}
	if !strings.Contains(got, "npm ERR! failed") {
		t.Errorf("summary = %q, want the text kept", got)
	}
}

func TestBlankLinesAreDroppedRatherThanCountedAsContent(t *testing.T) {
	// Otherwise three blank lines would be the whole summary.
	got := FailureDetail(map[string]string{}, "", "real error\n\n\n\n")
	if got != "real error" {
		t.Errorf("summary = %q, want the one real line", got)
	}
}

func TestAnEmptyFailureSummarisesToNothingRatherThanPunctuation(t *testing.T) {
	if got := FailureDetail(map[string]string{}, "", ""); got != "" {
		t.Errorf("summary = %q, want empty", got)
	}
}

func TestBothStreamsReachTheSummary(t *testing.T) {
	// Some installers report the cause on stdout.
	got := FailureDetail(map[string]string{}, "detail on stdout", "")
	if !strings.Contains(got, "detail on stdout") {
		t.Errorf("summary = %q, want stdout included", got)
	}
}

func TestTheVersionIsFoundInWhateverShapeAnAgentPrintsIt(t *testing.T) {
	for input, want := range map[string]string{
		"codex-cli 0.145.0":            "0.145.0",
		"0.145.0":                      "0.145.0",
		"claude 1.2.3 (build 4)":       "1.2.3",
		"Python 3.12.9":                "3.12.9",
		"v2.0.1":                       "2.0.1",
		"1.0.0-beta.2":                 "1.0.0-beta.2",
		"1.0.0+build.5":                "1.0.0+build.5",
		"banner line\nversion 9.8.7\n": "9.8.7",
		"no version here":              "",
		"":                             "",
		"1.2":                          "",
	} {
		if got := VersionFromOutput(input); got != want {
			t.Errorf("VersionFromOutput(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAVersionIsNotReadOutOfTheTailOfALongerNumber(t *testing.T) {
	// Python guards this with (?<!\d), which RE2 has no equivalent for, so the
	// preceding byte is checked after matching instead. What it prevents is
	// reading the tail of a number: without it "20260729.1.2.3" would yield
	// "1.2.3" from a date-like build stamp. A number that starts the match is
	// taken whole, which is what Python does too -- verified against it.
	if got := VersionFromOutput("12345.6.7"); got != "12345.6.7" {
		t.Errorf("got %q, want the whole number, as Python reads it", got)
	}
	// The match starts where the number does and takes the first three groups,
	// so a four-part stamp yields its first three rather than its last three.
	if got := VersionFromOutput("20260729.1.2.3"); got != "20260729.1.2" {
		t.Errorf("got %q, want the match anchored at the start of the number", got)
	}
	if strings.HasPrefix(VersionFromOutput("20260729.1.2.3"), "1.2") {
		t.Error("the tail of a longer number was read as a version")
	}
}
