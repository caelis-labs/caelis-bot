package websurface

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type retainedApprovalURLFixture struct {
	ids []string
	url string
	err error
}

func (f *retainedApprovalURLFixture) RetainedApprovalURL(id string) (string, bool, error) {
	f.ids = append(f.ids, id)
	return f.url, true, f.err
}

func TestApprovalURLReadRouteUsesRetainedOwnerAndDoesNotPublishRefusedURL(t *testing.T) {
	s := testServer(t)
	owner := &retainedApprovalURLFixture{url: "http://127.0.0.1:4444/private-native-challenge", err: errors.New("remote_native_url_not_reachable")}
	s.services.Backend.ConfigureWorkerInteractions(owner)
	c := pairClient(t, s)
	w := c.request("/api/v1/conversation/approval-url", map[string]any{"args": []string{"scoped-original-native-request"}})
	if w.Code != 503 || strings.Contains(w.Body.String(), "private-native-challenge") {
		t.Fatal("host-refused URL reached Web", w.Code, w.Body.String())
	}
	owner.url, owner.err = "https://approval.example.test/original-native-challenge", nil
	w = c.request("/api/v1/conversation/approval-url", map[string]any{"args": []string{"scoped-original-native-request"}})
	var response struct{ Value string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Value != owner.url {
		t.Fatal("host-validated original URL missing", w.Code, w.Body.String())
	}
	if len(owner.ids) != 2 || owner.ids[0] != "scoped-original-native-request" || owner.ids[1] != owner.ids[0] {
		t.Fatal("route changed original request identity", owner.ids)
	}
	if len(s.operations) != 0 {
		t.Fatal("URL read minted an effect or approval receipt")
	}
}

func TestApprovalURLNavigationRefusalPreservesPendingReason(t *testing.T) {
	s := testServer(t)
	owner := &retainedApprovalURLFixture{url: "http://127.0.0.1:4444/private-native-challenge", err: &api.ActionRejectedError{Code: "remote_native_url_not_reachable"}}
	s.services.Backend.ConfigureWorkerInteractions(owner)
	c := pairClient(t, s)
	w := c.request("/api/v1/conversation/approval-url", map[string]any{"args": []string{"original-still-pending"}})
	var result struct {
		Error string `json:"error"`
	}
	if w.Code != 503 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Error != "remote_native_url_not_reachable" || strings.Contains(w.Body.String(), owner.url) {
		t.Fatal("navigation refusal lost pending reason or leaked URL", w.Code, w.Body.String())
	}
	owner.err = &api.ActionRejectedError{Code: "private-native-diagnostic-must-not-leak"}
	w = c.request("/api/v1/conversation/approval-url", map[string]any{"args": []string{"original-still-pending"}})
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Error != "read_unavailable" || strings.Contains(w.Body.String(), "private-native-diagnostic") {
		t.Fatal("unapproved native detail published", w.Code, w.Body.String())
	}
	if len(s.operations) != 0 {
		t.Fatal("navigation read minted an execution/approval receipt")
	}
}
