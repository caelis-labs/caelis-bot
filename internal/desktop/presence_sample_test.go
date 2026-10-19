package desktop

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func splitJSONLines(raw []byte) [][]byte { return bytes.Split(bytes.TrimSpace(raw), []byte("\n")) }

func TestPresenceDecodeFailureIsUnknownAndDiagnosticsAreBounded(t *testing.T) {
	var records []diagnosticlog.Record
	d := presenceSampleDecoder{write: func(r diagnosticlog.Record) { records = append(records, r) }}
	for range 100 {
		s := d.decode([]byte(`{"Awake":true,"Unlocked":1,"Application":"private.application"}`))
		if s.Unlocked != nil || s.Available() {
			t.Fatal("invalid native Boolean became known")
		}
	}
	d.decode([]byte(`{"Application":"a-different-private-app","Unlocked":0}`))
	if len(records) != 1 || records[0].Code != "native_presence_invalid" {
		t.Fatal(records)
	}
	raw, _ := json.Marshal(records)
	if strings.Contains(string(raw), "private.application") {
		t.Fatal("private native data logged")
	}
	s := d.decode([]byte(`{"Awake":true,"Unlocked":null}`))
	if s.Unlocked != nil || len(records) != 1 {
		t.Fatal("genuine unknown was lost or logged as failure")
	}
	s = d.decode([]byte(`{"Awake":true,"Unlocked":true}`))
	if !s.Available() {
		t.Fatal("decoder did not recover")
	}
}
