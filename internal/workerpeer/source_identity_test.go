package workerpeer

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSharingIdentifiesSourcesWithoutPositionalLabels(t *testing.T) {
	s, h, _ := peerFixture(t)
	for _, source := range []struct{ id, name string }{{"source-z", "Office Mac"}, {"source-a", "Office Mac"}} {
		code, err := s.Pairing(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"peer": source.id, "name": source.name, "code": code.Code, "token": strings.Repeat("a", 48)})
		resp, err := http.Post(h.URL+Prefix+"pair", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal("pair", resp.Status)
		}
	}
	var previous string
	for range 20 {
		b, err := json.Marshal(s.Status())
		if err != nil {
			t.Fatal(err)
		}
		var view struct {
			Sources []struct {
				ID, Name string
				PairedAt int64
			}
		}
		if err = json.Unmarshal(b, &view); err != nil {
			t.Fatal("source identity is missing from settings", err)
		}
		if len(view.Sources) != 2 || view.Sources[0].ID != "source-a" || view.Sources[1].ID != "source-z" || view.Sources[0].Name != "Office Mac" || view.Sources[0].PairedAt == 0 {
			t.Fatal("source labels are not identifiable and stably sorted", string(b))
		}
		if previous != "" && previous != string(b) {
			t.Fatal("source order changed")
		}
		previous = string(b)
	}
}
