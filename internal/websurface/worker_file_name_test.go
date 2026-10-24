package websurface

import (
	"context"
	"errors"
	"mime"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

type namedWorkerCopyFixture struct {
	files   *workerfiles.Outputs
	offline atomic.Bool
	name    string
	data    []byte
}

func (f *namedWorkerCopyFixture) ReadTaskFile(_ context.Context, task, ref string) (api.WorkFileContent, error) {
	if task != "owned-task" || ref != "original-native-file" || f.offline.Load() {
		return api.WorkFileContent{}, errors.New("original_file_unavailable")
	}
	return api.WorkFileContent{Name: f.name, Data: f.data}, nil
}
func (f *namedWorkerCopyFixture) MaterializeTaskFile(ctx context.Context, task, ref string, refresh bool) (api.TaskFileCopy, error) {
	if task != "owned-task" || ref != "original-native-file" {
		return api.TaskFileCopy{}, errors.New("file_not_owned")
	}
	return f.files.Materialize(ctx, task, ref, refresh, func(ctx context.Context) (api.WorkFileContent, error) { return f.ReadTaskFile(ctx, task, ref) })
}
func TestWorkerFileHTTPKeepsVerifiedOriginalNameWithSafeCachePath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    []byte
		preview bool
	}{{"quarterly-report.csv", []byte("quarter,total\nQ1,42\n"), false}, {"quarterly-report.pdf", []byte("%PDF-1.4\nfixture"), true}, {`季度 "汇总".csv`, []byte("quarter,total\nQ2,19\n"), false}, {"page.html", []byte("<!doctype html><script>fixture()</script>"), true}, {"diagram.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`), true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &namedWorkerCopyFixture{files: workerfiles.OpenOutputs(t.TempDir()), name: tc.name, data: tc.data}
			s := testServer(t)
			s.services.Backend.ConfigureWorkerFiles(t.TempDir(), f)
			id := api.TaskArtifactID("owned-task", "original-native-file")
			path, err := backend.ResolveRemoteArtifact(s.services.Backend, id)
			if err != nil || filepath.Base(path) == tc.name {
				t.Fatal("safe cache path changed", path, err)
			}
			f.offline.Store(true)
			c := pairClient(t, s)
			get := func(reference string, paired bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", testOrigin+"/api/v1/artifacts/"+url.PathEscape(reference), nil)
				if tc.preview {
					r.URL.RawQuery = "preview=1"
				}
				if paired {
					r.AddCookie(c.cookie)
				}
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				return w
			}
			if get(id, false).Code != 401 || get(api.TaskArtifactID("another-task", "original-native-file"), true).Code != 404 {
				t.Fatal("file metadata bypassed browser authentication or original task ownership")
			}
			w := get(id, true)
			kind, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			want := "attachment"
			if tc.preview && filepath.Ext(tc.name) == ".pdf" {
				want = "inline"
			}
			if w.Code != 200 || err != nil || params["filename"] != tc.name || kind != want || w.Body.String() != string(tc.data) {
				t.Fatal("owned cached delivery lost original name or bytes", w.Code, kind, params, err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
				t.Fatal("delivery created another file with the native display name", err)
			}
			if err := os.WriteFile(path, []byte("corrupted cached fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if get(id, true).Code != 404 {
				t.Fatal("file name metadata bypassed cached byte verification")
			}
		})
	}
}
