package websurface

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func rejectedResponse(t *testing.T, w *httptest.ResponseRecorder, detail string) {
	t.Helper()
	var result map[string]any
	if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["error"] != "operation_rejected" || result["detail"] != detail {
		t.Fatalf("safe refusal lost terminal error or owning code: status=%d body=%s", w.Code, w.Body.String())
	}
}

func operationResponse(t *testing.T, s *Server, c *client, id string) map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", testOrigin+"/api/v1/operations/"+id, nil)
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var result map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("original operation could not be queried", w.Code, w.Body.String())
	}
	return result
}

func TestActionRejectionDetailSurvivesReplayQueryAndLegacyArchive(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	// A profile from before this change has the four-column archive. Upgrading
	// it must retain tombstones while adding bounded refusal presentation.
	db, err := sql.Open("sqlite", filepath.Join(s.config.Directory, "retired-operations.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE retired (id TEXT PRIMARY KEY, digest TEXT NOT NULL, owner TEXT NOT NULL, error TEXT NOT NULL) WITHOUT ROWID`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.register(action{Path: "/api/test/rejected-detail", Function: func() error {
		calls++
		return &api.ActionRejectedError{Code: "pairing_code_invalid"}
	}})
	body := map[string]any{"id": "original-rejected-detail", "args": []any{}}
	rejectedResponse(t, c.request("/api/test/rejected-detail", body), "pairing_code_invalid")
	original := s.operations["original-rejected-detail"]
	if original.Digest == "" || original.Owner != sessionIDFor(c) {
		t.Fatal("original Web intent lost identity")
	}
	check := func() {
		t.Helper()
		rejectedResponse(t, c.request("/api/test/rejected-detail", body), "pairing_code_invalid")
		query := operationResponse(t, s, c, "original-rejected-detail")
		if query["state"] != "complete" || query["error"] != "operation_rejected" || query["detail"] != "pairing_code_invalid" || calls != 1 {
			t.Fatal("query lost refusal detail or replayed original action", query, calls)
		}
	}
	check()
	s.operations = nil
	if err := s.loadOperations(); err != nil {
		t.Fatal(err)
	}
	check()
	for n := 0; n < 300; n++ {
		s.operations[fmt.Sprintf("later-terminal-%016d", n)] = operation{Owner: original.Owner, Digest: "fixture", State: "complete", At: original.At + 1}
	}
	if err := s.saveOperations(); err != nil {
		t.Fatal(err)
	}
	if _, retained := s.operations["original-rejected-detail"]; retained {
		t.Fatal("fixture did not exercise SQLite retirement")
	}
	if err := s.retired.Close(); err != nil {
		t.Fatal(err)
	}
	s.retired = nil
	check()
	retired, found, err := s.lookupOperation("original-rejected-detail")
	if err != nil || !found || retired.Digest != original.Digest || retired.Owner != original.Owner || !s.UpdateQuiescent() {
		t.Fatal("retirement changed original intent or terminal admission", retired, found, err)
	}
}

func TestActionRejectionDetailsAreAllowlistedAndOrdinaryFailuresStayUnknown(t *testing.T) {
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprint("typed-", typed), func(t *testing.T) {
			s := testServer(t)
			c := pairClient(t, s)
			calls := 0
			s.register(action{Path: "/api/test/private-refusal", Function: func() error {
				calls++
				if typed {
					return &api.ActionRejectedError{Code: "PRIVATE_UNALLOWLISTED_CODE"}
				}
				// Even a familiar message does not certify a business refusal.
				return errors.New("pairing_code_invalid")
			}})
			body := map[string]any{"id": "original-private-refusal", "args": []any{}}
			w := c.request("/api/test/private-refusal", body)
			want := "operation_unconfirmed"
			if typed {
				want = "operation_rejected"
			}
			query := operationResponse(t, s, c, "original-private-refusal")
			if w.Code != 409 || !strings.Contains(w.Body.String(), want) || query["error"] != want || query["detail"] != nil && query["detail"] != "" || strings.Contains(w.Body.String(), "PRIVATE_") {
				t.Fatal("private code leaked or ordinary error became a known rejection", w.Body.String(), query)
			}
			if c.request("/api/test/private-refusal", body).Code != 409 || calls != 1 || s.UpdateQuiescent() != typed {
				t.Fatal("rejection/uncertainty replay or terminal semantics changed", calls)
			}
		})
	}
}
