package jsonorder

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustParse(t *testing.T, raw string) *Object {
	t.Helper()
	object, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("cannot parse %q: %v", raw, err)
	}
	return object
}

func render(t *testing.T, object *Object) string {
	t.Helper()
	raw, err := Marshal(object)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	return string(raw)
}

func TestKeyOrderSurvivesARoundTrip(t *testing.T) {
	// A Go map would emit these alphabetically, rewriting the user's file with
	// their own keys rearranged.
	input := "{\n  \"zeta\": 1,\n  \"alpha\": 2,\n  \"middle\": 3\n}\n"
	if got := render(t, mustParse(t, input)); got != input {
		t.Fatalf("round trip changed the file:\n  got:  %q\n  want: %q", got, input)
	}
}

func TestSettingAnExistingKeyUpdatesItInPlace(t *testing.T) {
	// This is what keeps ANTHROPIC_MODEL where the user had it instead of moving
	// it to the end alongside the keys OneAgent adds.
	object := mustParse(t, `{"first":1,"target":"old","last":3}`)
	object.Set("target", "new")
	if keys := strings.Join(object.Keys(), ","); keys != "first,target,last" {
		t.Fatalf("keys = %s, want the original order", keys)
	}
	if object.GetString("target") != "new" {
		t.Error("the value was not updated")
	}
}

func TestANewKeyAppendsRatherThanSorting(t *testing.T) {
	object := mustParse(t, `{"beta":1}`)
	object.Set("alpha", 2)
	if keys := strings.Join(object.Keys(), ","); keys != "beta,alpha" {
		t.Fatalf("keys = %s, want insertion order", keys)
	}
}

func TestDeletingAKeyLeavesTheRestInOrder(t *testing.T) {
	object := mustParse(t, `{"a":1,"b":2,"c":3}`)
	object.Delete("b")
	if keys := strings.Join(object.Keys(), ","); keys != "a,c" {
		t.Fatalf("keys = %s", keys)
	}
	object.Delete("not-present")
	if object.Len() != 2 {
		t.Error("deleting an absent key changed the object")
	}
}

func TestHTMLCharactersAreNotEscaped(t *testing.T) {
	// Go's default encoder writes & for &, which Python does not. A base URL
	// with a query string is the realistic way one reaches a config file.
	object := NewObject()
	object.Set("baseUrl", "https://x.test/?a=1&b=2<c>")
	got := render(t, object)
	// Written as escapes so this file does not contain the literal sequences it
	// is asserting the absence of.
	for _, escaped := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if strings.Contains(got, escaped) {
			t.Fatalf("output escaped an HTML character (%s): %s", escaped, got)
		}
	}
	if !strings.Contains(got, "a=1&b=2<c>") {
		t.Fatalf("output = %s, want the characters verbatim", got)
	}
}

func TestNonASCIIStaysAsUTF8(t *testing.T) {
	// ensure_ascii=False. Escaping to \uXXXX would be valid JSON and a different
	// file from the one Python writes.
	object := NewObject()
	object.Set("model", "通义-max")
	got := render(t, object)
	if !strings.Contains(got, "通义-max") {
		t.Fatalf("output = %s, want the characters verbatim", got)
	}
}

func TestControlCharactersAreEscapedInLowerCaseHex(t *testing.T) {
	// Python escapes control characters with lower-case hex. Upper-case would be
	// valid JSON and a byte difference from the file Python writes.
	object := NewObject()
	object.Set("value", "\x01\x1f")
	got := render(t, object)
	for _, want := range []string{`\u0001`, `\u001f`} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %s, want %s", got, want)
		}
	}
	if strings.Contains(got, `\u001F`) {
		t.Errorf("output = %s, want lower-case hex", got)
	}
}

func TestTheOutputEndsWithExactlyOneNewline(t *testing.T) {
	// MarshalIndent omits it; the config files carry it.
	got := render(t, mustParse(t, `{"a":1}`))
	if !strings.HasSuffix(got, "}\n") {
		t.Fatalf("output = %q, want a trailing newline", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Fatalf("output = %q, want exactly one", got)
	}
}

func TestIndentationIsTwoSpacesAtEveryDepth(t *testing.T) {
	object := mustParse(t, `{"outer":{"inner":{"deep":1}}}`)
	want := "{\n  \"outer\": {\n    \"inner\": {\n      \"deep\": 1\n    }\n  }\n}\n"
	if got := render(t, object); got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestAnEmptyObjectAndArrayRenderCompactly(t *testing.T) {
	// json.dumps writes {} and [] on one line even with indent set.
	object := mustParse(t, `{"empty":{},"list":[]}`)
	want := "{\n  \"empty\": {},\n  \"list\": []\n}\n"
	if got := render(t, object); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestIntegersAndPlainDecimalsKeepTheirOriginalText(t *testing.T) {
	// Decoding into float64 would turn 1 into 1, 1.0 into 1 and a large
	// integer into scientific notation -- all of which rewrite a value the user
	// set. Exponent forms are the exception and have their own test below,
	// because Python does not preserve those either.
	input := "{\n  \"int\": 1,\n  \"float\": 1.5,\n  \"big\": 12345678901234567890,\n  \"zero\": 0.0,\n  \"neg\": -2\n}\n"
	if got := render(t, mustParse(t, input)); got != input {
		t.Fatalf("numbers changed:\n  got:  %s\n  want: %s", got, input)
	}
}

func TestAnExponentFormIsRewrittenTheWayPythonRewritesIt(t *testing.T) {
	// This asserted the opposite until the byte comparison with Python showed
	// otherwise: json.loads promotes any exponent-carrying number to a float and
	// json.dumps writes repr() of it, so preserving the original text would be a
	// difference from the file Python produces. number_parity_test.go holds both
	// sides to this across the range.
	input := "{\n  \"exp\": 1e10\n}\n"
	want := "{\n  \"exp\": 10000000000.0\n}\n"
	if got := render(t, mustParse(t, input)); got != want {
		t.Fatalf("got:  %s\nwant: %s", got, want)
	}
}

func TestNestedArraysOfObjectsKeepTheirShape(t *testing.T) {
	input := "{\n  \"items\": [\n    {\n      \"b\": 1,\n      \"a\": 2\n    },\n    \"text\",\n    null,\n    true\n  ]\n}\n"
	if got := render(t, mustParse(t, input)); got != input {
		t.Fatalf("output:\n%s\nwant:\n%s", got, input)
	}
}

func TestChildCreatesAMissingObjectAndReusesAnExistingOne(t *testing.T) {
	object := mustParse(t, `{"env":{"EXISTING":"1"}}`)
	env, err := object.Child("env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env.Set("ADDED", "2")
	if got := object.GetString("env"); got != "" {
		t.Error("env should be an object, not a string")
	}
	nested, present := object.GetObject("env")
	if !present || nested.GetString("EXISTING") != "1" || nested.GetString("ADDED") != "2" {
		t.Errorf("nested object = %v", nested)
	}

	fresh := NewObject()
	child, err := fresh.Child("new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	child.Set("k", "v")
	if _, present := fresh.GetObject("new"); !present {
		t.Error("Child did not attach the new object")
	}
}

func TestChildRefusesToOverwriteANonObjectValue(t *testing.T) {
	// The user put something else there. Replacing it with a table would lose
	// whatever the file was carrying, which is what CONFIG_WRITE_FAILED exists
	// to prevent.
	for _, raw := range []string{
		`{"env":"not-an-object"}`,
		`{"env":[]}`,
		`{"env":42}`,
		`{"env":true}`,
	} {
		object := mustParse(t, raw)
		if _, err := object.Child("env"); err == nil {
			t.Errorf("%s: Child should have refused", raw)
		}
	}
}

func TestANullValueIsTreatedAsAbsentByChild(t *testing.T) {
	// A key explicitly set to null carries no data to lose.
	object := mustParse(t, `{"env":null}`)
	child, err := object.Child("env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	child.Set("k", "v")
	if nested, present := object.GetObject("env"); !present || nested.GetString("k") != "v" {
		t.Error("the null value was not replaced with an object")
	}
}

func TestAWronglyTypedFieldReadsAsAbsentRatherThanStringifying(t *testing.T) {
	// Config files are user-editable, so any field can be the wrong shape.
	object := mustParse(t, `{"model":42,"other":true,"nested":{}}`)
	if got := object.GetString("model"); got != "" {
		t.Errorf("GetString on a number = %q, want empty", got)
	}
	if got := object.GetString("other"); got != "" {
		t.Errorf("GetString on a boolean = %q, want empty", got)
	}
	if got := object.GetString("missing"); got != "" {
		t.Errorf("GetString on an absent key = %q, want empty", got)
	}
	if _, present := object.GetObject("model"); present {
		t.Error("GetObject on a number should report absent")
	}
}

func TestParsingSomethingThatIsNotAnObjectIsRefused(t *testing.T) {
	for _, raw := range []string{`[]`, `"text"`, `42`, `null`, `{not json`, ``} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%q should be refused", raw)
		}
	}
}

func TestOperationsOnANilObjectDoNotPanic(t *testing.T) {
	// Reached when a caller holds the result of a failed GetObject.
	var object *Object
	if object.Len() != 0 || object.Keys() != nil {
		t.Error("a nil object should read as empty")
	}
	if _, present := object.Get("k"); present {
		t.Error("a nil object should hold nothing")
	}
	if object.GetString("k") != "" {
		t.Error("GetString on nil should be empty")
	}
	object.Delete("k")
	if got := render(t, object); got != "{}\n" {
		t.Errorf("marshalling nil = %q, want {}", got)
	}
}

func TestValuesTheCoreSetsItselfSerialiseCorrectly(t *testing.T) {
	// The adapters set Go strings, ints and nested objects rather than
	// json.Number, so that path needs its own coverage.
	object := NewObject()
	object.Set("text", "value")
	object.Set("count", 3)
	object.Set("ratio", 1.5)
	object.Set("flag", true)
	object.Set("nothing", nil)
	got := render(t, object)
	var check map[string]any
	if err := json.Unmarshal([]byte(got), &check); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}
	if check["count"] != float64(3) || check["ratio"] != 1.5 || check["flag"] != true {
		t.Errorf("values did not survive: %v", check)
	}
}

func TestSortedKeysIsAvailableForDiagnosticsWithoutAffectingOutput(t *testing.T) {
	object := mustParse(t, `{"zeta":1,"alpha":2}`)
	if got := strings.Join(SortedKeys(object), ","); got != "alpha,zeta" {
		t.Errorf("SortedKeys = %s", got)
	}
	if got := strings.Join(object.Keys(), ","); got != "zeta,alpha" {
		t.Errorf("SortedKeys mutated the object: %s", got)
	}
}

func TestAnObjectNestedInAStructEncodesItsContents(t *testing.T) {
	// The failure this exists for: the fields are unexported, so without a
	// MarshalJSON encoding/json finds nothing to serialise and emits {} rather than
	// failing. An Object reached the status payload through a struct field and every
	// stored profile came out empty, which read as a migration bug.
	object := NewObject()
	object.Set("second", "b")
	object.Set("first", "a")

	var wrapper struct {
		Profile *Object `json:"profile"`
		Absent  *Object `json:"absent"`
	}
	wrapper.Profile = object

	encoded, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	text := string(encoded)
	for _, fragment := range []string{`"second"`, `"b"`, `"first"`, `"a"`} {
		if !strings.Contains(text, fragment) {
			t.Errorf("the nested object lost %s: %s", fragment, text)
		}
	}
	// Insertion order survives the nesting, which is the whole point of the type.
	if strings.Index(text, "second") > strings.Index(text, `"first"`) {
		t.Errorf("keys were reordered: %s", text)
	}
	// A nil object encodes as null rather than panicking.
	if !strings.Contains(text, `"absent":null`) {
		t.Errorf("a nil object did not encode as null: %s", text)
	}
}
