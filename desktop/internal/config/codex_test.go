package config

import (
	"strings"
	"testing"
)

// The merge is line-based, and these two refusals are what keeps that honest: a
// key or table the parser can see but the line scanner cannot would be
// duplicated rather than replaced, leaving a file with two conflicting values.

func TestATableDeclaredInADottedFormIsRefusedRatherThanDuplicated(t *testing.T) {
	// model_providers.oneagent as an inline table is valid TOML that this merge
	// cannot find by scanning for a [header]. Proceeding would append a second
	// declaration of the same table.
	for name, existing := range map[string]string{
		"inline table":     "model_providers = { oneagent = { name = \"X\" } }\n",
		"dotted key":       "model_providers.oneagent.name = \"X\"\n",
		"dotted sub-table": "[model_providers]\noneagent = { name = \"X\" }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := MergeCodexTOML(existing, "model_provider = \"oneagent\"\n", "config.toml")
			if err == nil {
				t.Fatal("this syntax should be refused")
			}
			assertCode(t, err, "CONFIG_WRITE_FAILED")
			if !strings.Contains(err.Error(), "Unsupported") {
				t.Errorf("message = %q, want it to say the syntax is unsupported", err.Error())
			}
		})
	}
}

func TestAManagedKeyTheScannerCannotSeeIsRefused(t *testing.T) {
	// A multi-line or otherwise unusual form of model/model_provider: the parser
	// finds it, the scanner does not remove it, so the merge would emit two.
	for name, existing := range map[string]string{
		"multi-line string":         "model = \"\"\"\ngpt-5\n\"\"\"\n",
		"model_provider multi-line": "model_provider = \"\"\"\noneagent\n\"\"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := MergeCodexTOML(existing, "model = \"new\"\n", "config.toml")
			if err == nil {
				t.Fatal("this syntax should be refused")
			}
			assertCode(t, err, "CONFIG_WRITE_FAILED")
		})
	}
}

func TestAPlainHeaderDeclarationOfOurTableIsReplacedNotRefused(t *testing.T) {
	// The normal case: OneAgent has written this file before, so its table is
	// there in the form the scanner recognises and is replaced in place.
	existing := "model_provider = \"oneagent\"\nmodel = \"old\"\n\n" +
		"[model_providers.oneagent]\nname = \"Old\"\nbase_url = \"https://old\"\n\n" +
		"[other]\nkeep = true\n"
	managed := "model_provider = \"oneagent\"\nmodel = \"new\"\n\n[model_providers.oneagent]\nname = \"New\"\n"

	merged, err := MergeCodexTOML(existing, managed, "config.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Count(merged, "[model_providers.oneagent]") != 1 {
		t.Errorf("our table appears %d times:\n%s", strings.Count(merged, "[model_providers.oneagent]"), merged)
	}
	if strings.Contains(merged, `name = "Old"`) {
		t.Errorf("the old table survived:\n%s", merged)
	}
	if !strings.Contains(merged, "[other]") || !strings.Contains(merged, "keep = true") {
		t.Errorf("an unrelated table was dropped:\n%s", merged)
	}
}

func TestASubTableOfOursIsAlsoReplaced(t *testing.T) {
	// [model_providers.oneagent.something] belongs to our table, so leaving it
	// behind would attach stale settings to the new provider definition.
	existing := "[model_providers.oneagent]\nname = \"Old\"\n\n" +
		"[model_providers.oneagent.extra]\nkey = \"stale\"\n\n[keep]\nx = 1\n"
	merged, err := MergeCodexTOML(existing, "[model_providers.oneagent]\nname = \"New\"\n", "config.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(merged, "stale") {
		t.Errorf("a stale sub-table survived:\n%s", merged)
	}
	if !strings.Contains(merged, "[keep]") {
		t.Errorf("an unrelated table was dropped:\n%s", merged)
	}
}

func TestAnEmptyFileIsReplacedWholesale(t *testing.T) {
	for _, existing := range []string{"", "   ", "\n\n\t\n"} {
		managed := "model = \"m\"\n"
		merged, err := MergeCodexTOML(existing, managed, "config.toml")
		if err != nil {
			t.Fatalf("existing=%q: unexpected error %v", existing, err)
		}
		if merged != managed {
			t.Errorf("existing=%q: merged = %q, want the managed block alone", existing, merged)
		}
	}
}

func TestAMergeThatWouldProduceInvalidTOMLIsRefused(t *testing.T) {
	// The read-back is what catches a combination that is individually valid but
	// broken together -- a duplicate table, for instance. Without it the Agent
	// would be left with a file it cannot parse.
	existing := "[tui]\ntheme = \"dark\"\n"
	// A managed block redeclaring the same table.
	managed := "[tui]\ntheme = \"light\"\n"
	if _, err := MergeCodexTOML(existing, managed, "config.toml"); err == nil {
		t.Fatal("a merge producing a duplicate table should be refused")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestTheManagedBlockLandsBetweenTopLevelKeysAndTables(t *testing.T) {
	// TOML scoping: a bare key after a [table] header belongs to that table, so
	// our top-level keys have to precede every header or they would silently
	// become part of someone else's table.
	existing := "top_level = 1\n\n[a_table]\nkey = 2\n"
	managed := "model_provider = \"oneagent\"\n\n[model_providers.oneagent]\nname = \"X\"\n"
	merged, err := MergeCodexTOML(existing, managed, "config.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	topLevelAt := strings.Index(merged, "top_level = 1")
	ourKeyAt := strings.Index(merged, "model_provider =")
	tableAt := strings.Index(merged, "[a_table]")
	if !(topLevelAt < ourKeyAt && ourKeyAt < tableAt) {
		t.Fatalf("order is wrong; a bare key would fall inside a table:\n%s", merged)
	}
}

func TestACommentAfterATableHeaderDoesNotConfuseTheScanner(t *testing.T) {
	existing := "[model_providers.oneagent]  # ours\nname = \"Old\"\n\n[keep]  # theirs\nx = 1\n"
	merged, err := MergeCodexTOML(existing, "[model_providers.oneagent]\nname = \"New\"\n", "config.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(merged, `name = "Old"`) {
		t.Errorf("our old table was not recognised:\n%s", merged)
	}
	if !strings.Contains(merged, "[keep]") {
		t.Errorf("their table was dropped:\n%s", merged)
	}
}

func TestSpacingInsideATableHeaderIsNormalisedBeforeComparison(t *testing.T) {
	// "[ model_providers . oneagent ]" is the same table as "[model_providers.oneagent]".
	existing := "[ model_providers . oneagent ]\nname = \"Old\"\n"
	merged, err := MergeCodexTOML(existing, "[model_providers.oneagent]\nname = \"New\"\n", "config.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(merged, `name = "Old"`) {
		t.Errorf("the spaced header was not recognised as ours:\n%s", merged)
	}
}

func TestTheManagedBlockDeclaresTheOnlyWireApiCodexSpeaks(t *testing.T) {
	// Fixed rather than configurable: Codex speaks the Responses API. Offering a
	// choice would let a user select one that cannot work.
	block := CodexManagedBlock("PPIO", "https://x.test", "m", "ONEAGENT_API_KEY_CODEX")
	if !strings.Contains(block, `wire_api = "responses"`) {
		t.Errorf("block = %q, want wire_api fixed to responses", block)
	}
}

func TestTheManagedBlockReferencesTheKeyByVariableRatherThanEmbeddingIt(t *testing.T) {
	block := CodexManagedBlock("PPIO", "https://x.test", "m", "ONEAGENT_API_KEY_CODEX")
	if !strings.Contains(block, `env_key = "ONEAGENT_API_KEY_CODEX"`) {
		t.Errorf("block = %q, want an env_key reference", block)
	}
}

func TestTOMLStringsEscapeTheWayThePythonAdapterDoes(t *testing.T) {
	// This adapter quotes with json.dumps and no ensure_ascii=False, unlike the
	// JSON adapters, so non-ASCII becomes an escape sequence here while the same
	// value stays UTF-8 in settings.json. Using one helper for both would produce
	// a file that differs from Python's on exactly the inputs nobody tests with.
	for input, want := range map[string]string{
		"plain":     `"plain"`,
		`has"quote`: `"has\"quote"`,
		`has\back`:  `"has\\back"`,
		"tab\there": `"tab\there"`,
		"new\nline": `"new\nline"`,
		"return\rx": `"return\rx"`,
		"":          `""`,
		// Non-ASCII is escaped, which is the property that differs from the JSON
		// adapters. Written as \u escapes so this file holds no literal ones.
		"\u901a\u4e49-max": `"\u901a\u4e49-max"`,
		"emoji\u2705":      `"emoji\u2705"`,
		"\x01":             `"\u0001"`,
	} {
		if got := tomlString(input); got != want {
			t.Errorf("tomlString(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestAstralCharactersBecomeASurrogatePair(t *testing.T) {
	// json.dumps targets UTF-16 escapes, so anything outside the BMP is written
	// as two escapes rather than one -- which a naive %04x would get wrong.
	if got := tomlString("\U0001D11E"); got != `"\ud834\udd1e"` {
		t.Errorf("got %s, want a surrogate pair", got)
	}
}

func TestSplitLinesHandlesEveryLineEndingAConfigMightUse(t *testing.T) {
	for input, want := range map[string]int{
		"a\nb\n":     2,
		"a\nb":       2,
		"a\r\nb\r\n": 2,
		"a\rb\r":     2,
		"":           0,
		"\n":         1,
		"a":          1,
	} {
		if got := len(splitLines(input)); got != want {
			t.Errorf("splitLines(%q) gave %d lines, want %d", input, got, want)
		}
	}
}
