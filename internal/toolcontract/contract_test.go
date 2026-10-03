package toolcontract

import (
	"encoding/json"
	"github.com/caelis-labs/desktop-world/protocol"
	"strings"
	"testing"
)

// These are the native contract operators, rather than a second general-purpose
// JSON Schema engine. Invalid cross-field combinations must stop at the host.
func TestNativeGrammar(t *testing.T) {
	native := protocol.ArgumentsSchema("world.act")
	native["required"] = []string{"steps"}
	schema, _ := json.Marshal(native)
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{"steps":[{"id":"s","op":"invoke","target":{"ref":"known"}}]}`, true},
		{`{"steps":[{"id":"s","op":"keyboard.type_text","target":{"ref":"known"},"type_text":{"text":"hello"}}]}`, true},
		{`{"steps":[{"id":"s","op":"set_value","target":{"ref":"known"},"set_value":{"text":"hello"},"press":{"key":"Enter"}}]}`, false},
		{`{"steps":[{"id":"s","op":"invoke","target":{"ref":"known","locator":{"name":"guess"}}}]}`, false},
		{`{"steps":[]}`, false},
		{`{"steps":[],"steps":[]}`, false},
		{`{"steps":[]} {}`, false},
	} {
		_, err := Decode(schema, json.RawMessage(test.raw))
		if (err == nil) != test.valid {
			t.Errorf("valid=%t err=%v raw=%s", test.valid, err, test.raw)
		}
	}
}
func TestVariantErrorsAndBounds(t *testing.T) {
	schema, _ := json.Marshal(Request(Branch("read", Schema{"id": Schema{"type": "string", "minLength": 1}}, "id"), Branch("save", Schema{"n": Integer(1, 5)}, "n")))
	for _, raw := range []string{`{"request":{"type":"save","n":6}}`, `{"request":{"type":"save","n":2.5}}`, `{"request":{"type":"save","n":2,"id":"extra"}}`, `{"request":{"type":"read","id":""}}`, `{"request":null}`} {
		if _, e := Decode(schema, json.RawMessage(raw)); e == nil {
			t.Fatal("invalid accepted", raw)
		}
	}
	_, e := Decode(schema, json.RawMessage(`{"request":{"type":"save","n":6}}`))
	if strings.Contains(e.Error(), "missing id") {
		t.Fatal("unrelated branch noise", e)
	}
	if _, e := Decode(schema, json.RawMessage(`{"request":{"type":"read","id":"x"}}`)); e != nil {
		t.Fatal(e)
	}
}
