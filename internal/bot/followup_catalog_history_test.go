package bot

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestV0183FrozenCatalogAndUnboundAnswerHasNoEffect(t *testing.T) {
	raw, err := historicalCatalogs.ReadFile("catalog_history/pre-followup.json")
	if err != nil {
		t.Fatal(err)
	}
	// Exported from untouched 52e5d7a2, not reconstructed from current definitions.
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "e1fea792ffc0028fb87e87469aeae3292c3d48dd6fc839a94c6cf032647c2fa5" {
		t.Fatal("v0183 directory bytes changed")
	}
	r, _, _ := fixture(t)
	f := &workerInteractionFixture{}
	r.ConfigureWorkerInteractions(f)
	port := priorFollowupTools{r}
	out := port.CallTool(t.Context(), "bot_interactions", []byte(`{"request":{"type":"answer","question":"Q7","value":"2","requestId":"original-v0183-request"}}`))
	if !out.IsError || f.answer != "" {
		t.Fatal("old schema guessed a recycled question binding")
	}
	found := false
	for _, c := range r.HistoricalToolConnections() {
		for _, d := range c.Host.Definitions() {
			if d.Name == "bot_interactions" && d.Description == port.Definitions()[3].Description {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("v0183 callback catalog not registered")
	}
}
