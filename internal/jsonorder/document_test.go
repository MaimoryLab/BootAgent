package jsonorder

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRoundTripPreservesOrderAndPythonStringEncoding(t *testing.T) {
	input := `{"zeta":1,"alpha":"通义-max","nested":{"b":true,"a":null},"items":[{"x":1},2]}`
	object, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"zeta\": 1,\n  \"alpha\": \"通义-max\",\n  \"nested\": {\n    \"b\": true,\n    \"a\": null\n  },\n  \"items\": [\n    {\n      \"x\": 1\n    },\n    2\n  ]\n}\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestExistingKeysUpdateInPlaceAndNewKeysAppend(t *testing.T) {
	object, err := Parse([]byte(`{"first":1,"target":"old","last":3}`))
	if err != nil {
		t.Fatal(err)
	}
	object.Set("target", "new")
	object.Set("added", 4)
	if got := strings.Join(object.Keys(), ","); got != "first,target,last,added" {
		t.Fatalf("keys = %s", got)
	}
}

func TestChildRejectsWrongTypeAndCreatesMissingObject(t *testing.T) {
	object, err := Parse([]byte(`{"env":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := object.Child("env"); err == nil {
		t.Fatal("expected wrong-type error")
	}
	fresh := NewObject()
	child, err := fresh.Child("env")
	if err != nil {
		t.Fatal(err)
	}
	child.Set("KEY", "value")
	if got, ok := fresh.GetObject("env"); !ok || got.GetString("KEY") != "value" {
		t.Fatalf("child was not attached: %#v", fresh)
	}
}

func TestMarshalJSONWorksWhenNestedInStruct(t *testing.T) {
	object := NewObject()
	object.Set("second", "b")
	object.Set("first", "a")
	value, err := json.Marshal(struct {
		Profile *Object `json:"profile"`
	}{Profile: object})
	if err != nil {
		t.Fatal(err)
	}
	text := string(value)
	if strings.Index(text, "second") > strings.Index(text, "first") {
		t.Fatalf("nested object was reordered: %s", text)
	}
}

func TestExponentNumbersMatchPythonStyleRendering(t *testing.T) {
	object, err := Parse([]byte(`{"large":1e10,"small":1e-5,"plain":1.5}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"large": 10000000000.0`) || !strings.Contains(string(got), `"small": 1e-05`) {
		t.Fatalf("numbers were not rendered like Python: %s", got)
	}
}
