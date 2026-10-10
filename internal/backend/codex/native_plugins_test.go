package codex

import "testing"

func TestProjectNativePluginsOnlyInstalledEnabled(t *testing.T) {
	raw := []byte(`{"marketplaces":[{"plugins":[{"id":"ready","name":"raw","installed":true,"enabled":true,"interface":{"displayName":"Useful","shortDescription":"Preview"}},{"id":"off","name":"Off","installed":true,"enabled":false},{"id":"remote","name":"Remote","installed":false,"enabled":true}]},{"plugins":[{"id":"ready","name":"duplicate","installed":true,"enabled":true}]}]}`)
	got, err := projectNativePlugins(raw)
	if err != nil || len(got) != 1 || got[0].ID != "ready" || got[0].Name != "Useful" || got[0].Description != "Preview" || got[0].Source != "codex" {
		t.Fatalf("plugins = %+v, %v", got, err)
	}
}
