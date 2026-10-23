package workerpeer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerDispatchDoesNotReplaceOriginalFileDigest(t *testing.T) {
	var writes atomic.Int32
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes.Add(1) }))
	defer h.Close()
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	files := []api.InputFile{{Name: "a.csv", Path: path}}
	digest, err := api.WorkInputDigests(files)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed after preflight"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"start", "send"} {
		_, err = Call(t.Context(), Connection{Address: h.URL, Token: "fixture-proof"}, Request{Action: action, Start: api.WorkStart{TaskStart: api.TaskStart{Files: files, FileDigests: digest}}, Message: api.TaskMessage{Files: files, FileDigests: digest}})
		var rejected *api.WorkMessageRejection
		if err == nil || errors.As(err, &rejected) || writes.Load() != 0 {
			t.Fatal("changed file rewrote original native intent or settled a prior unknown effect", action, err, writes.Load())
		}
	}
}
func TestWorkerFileHTTPRetainsCancellationAsUnknown(t *testing.T) {
	entered := make(chan struct{})
	ended := make(chan struct{})
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(ended)
	}))
	defer h.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Call(ctx, Connection{Address: h.URL, Token: "fixture-proof"}, Request{Action: "send", Message: api.TaskMessage{ID: "original-task", RequestID: "original-op", Prompt: "continue"}})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("upload not observed")
	}
	cancel()
	select {
	case err := <-done:
		var rejected *api.WorkMessageRejection
		if err == nil || errors.As(err, &rejected) {
			t.Fatal("cancelled write became success/rejection", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop HTTP")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("B did not observe disconnect")
	}
}
func TestWorkerBodyBudgetOverridesHeadlessReadTimeoutOnlyAfterAuthentication(t *testing.T) {
	owner := newFixtureOwner()
	s, err := Open(t.TempDir(), func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
		return owner, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Enable(true); err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	h := httptest.NewUnstartedServer(s)
	h.Config.ReadTimeout = 30 * time.Millisecond
	h.Start()
	defer h.Close()
	c := pairFixture(t, s, h, "source-a")
	raw, _ := json.Marshal(Request{Contract: api.WorkerContract, Action: "info"})
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		defer writer.Close()
		_, _ = writer.Write(raw[:1])
		time.Sleep(100 * time.Millisecond)
		_, _ = writer.Write(raw[1:])
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", h.URL+Prefix+"call", reader)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out Response
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 || out.Error != "" {
		t.Fatal("existing 45s headless-style reader still truncates file body", resp.StatusCode, err, out.Error)
	}
	req, _ = http.NewRequestWithContext(ctx, "POST", h.URL+Prefix+"call", bytes.NewReader(raw))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("file allowance relaxed peer authentication", resp.StatusCode)
	}
}

func TestWorkerMetadataDoesNotTrustOldEmptyValidationReply(t *testing.T) {
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{Contract: api.WorkerContract, Peer: "original-peer"})
	}))
	defer h.Close()
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("a,b"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Call(t.Context(), Connection{Address: h.URL, Peer: "original-peer", Token: "fixture-proof"}, Request{Action: "validate_files", Message: api.TaskMessage{Files: []api.InputFile{{Name: "a.csv", Path: path}}}})
	if err == nil || err.Error() != "worker_input_limits_unavailable" {
		t.Fatal("old reply ignored metadata and claimed preflight success", err)
	}
}
