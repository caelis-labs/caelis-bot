package weixin

import (
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type fileFixture struct {
	mu           sync.Mutex
	server       *httptest.Server
	keys         map[string][]byte
	md5s         map[string]string
	uploaded     map[string][]byte
	items        []messageItem
	uploads      int
	sendStatus   int
	uploadRet    int
	uploadStatus int
	uploadGate   chan struct{}
	sendRet      int
}

func newFileFixture(t *testing.T) *fileFixture {
	t.Helper()
	f := &fileFixture{keys: map[string][]byte{}, md5s: map[string]string{}, uploaded: map[string][]byte{}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			var req struct {
				Key        string `json:"filekey"`
				Type       int    `json:"media_type"`
				Owner      string `json:"to_user_id"`
				RawSize    int    `json:"rawsize"`
				MD5        string `json:"rawfilemd5"`
				CipherSize int    `json:"filesize"`
				NoThumb    bool   `json:"no_need_thumb"`
				AESKey     string `json:"aeskey"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Type != 3 || req.Owner != "owner" || !req.NoThumb || req.CipherSize != (req.RawSize/16+1)*16 || len(req.MD5) != 32 {
				t.Errorf("getuploadurl fields: %+v", req)
			}
			key, err := hex.DecodeString(req.AESKey)
			if err != nil || len(key) != 16 {
				t.Errorf("AES key format: %v", err)
			}
			f.mu.Lock()
			f.keys[req.Key] = key
			f.md5s[req.Key] = req.MD5
			rejected := f.uploadRet
			f.mu.Unlock()
			if rejected != 0 {
				_, _ = w.Write([]byte(`{"ret":-2,"errmsg":"private server detail"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"upload_full_url": f.server.URL + "/c2c/upload?filekey=" + req.Key})
		case "/c2c/upload":
			if r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Error("CDN received credential or wrong type")
			}
			ciphertext, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			key := f.keys[r.URL.Query().Get("filekey")]
			wantMD5 := f.md5s[r.URL.Query().Get("filekey")]
			f.uploads++
			status := f.uploadStatus
			gate := f.uploadGate
			f.mu.Unlock()
			if status != 0 {
				if status == http.StatusFound {
					w.Header().Set("Location", "https://evil.test/upload")
				}
				w.WriteHeader(status)
				return
			}
			if gate != nil {
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
			}
			plain := decryptFixture(t, ciphertext, key)
			checksum := md5.Sum(plain)
			if hex.EncodeToString(checksum[:]) != wantMD5 {
				t.Error("plaintext checksum mismatch")
			}
			f.mu.Lock()
			f.uploaded[r.URL.Query().Get("filekey")] = plain
			f.mu.Unlock()
			w.Header().Set("x-encrypted-param", "download-"+r.URL.Query().Get("filekey"))
			w.WriteHeader(http.StatusOK)
		case "/ilink/bot/sendmessage":
			var req struct {
				Msg struct {
					To    string        `json:"to_user_id"`
					Token string        `json:"context_token"`
					Items []messageItem `json:"item_list"`
				} `json:"msg"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Msg.To != "owner" || req.Msg.Token != "ctx" || len(req.Msg.Items) != 1 {
				t.Errorf("send envelope: %+v", req)
			}
			f.mu.Lock()
			f.items = append(f.items, req.Msg.Items...)
			status, ret := f.sendStatus, f.sendRet
			f.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			if ret != 0 {
				_, _ = w.Write([]byte(`{"ret":-2,"errmsg":"private server detail"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ret":0,"message_id":18446744073709551615}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func decryptFixture(t *testing.T, ciphertext, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil || len(ciphertext)%aes.BlockSize != 0 || len(ciphertext) == 0 {
		t.Errorf("cipher format: %v", err)
		return nil
	}
	plain := append([]byte(nil), ciphertext...)
	for i := range len(plain) / aes.BlockSize {
		block.Decrypt(plain[i*16:(i+1)*16], plain[i*16:(i+1)*16])
	}
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > 16 {
		t.Errorf("bad PKCS7 padding %d", pad)
		return nil
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			t.Error("bad padding bytes")
			return nil
		}
	}
	return plain[:len(plain)-pad]
}

func (f *fileFixture) protocol() *protocol {
	return &protocol{client: f.server.Client(), base: f.server.URL, cdnBase: f.server.URL + "/c2c", token: "fixture"}
}

func waitFile(t *testing.T, b *Bridge, id string) outbound {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		b.mu.Lock()
		record := b.state.Outputs[artifactKey(id)]
		active := b.fileWork[artifactKey(id)]
		b.mu.Unlock()
		if record.State != "" && record.State != "preparing" && !active {
			return record
		}
		select {
		case <-deadline:
			t.Fatalf("file %s stuck: %+v", id, record)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func fileSnapshot(artifacts []api.Artifact, body string) api.Snapshot {
	return api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "completed", Items: []api.Item{
		{ID: "user", Kind: "user", TurnKey: "turn", RequestID: "input", Status: "completed"},
		{ID: "answer", Kind: "assistant", TurnKey: "turn", Text: body, Artifacts: artifacts, Status: "completed"},
	}}
}

func fileBridge(t *testing.T, root string, snapshot api.Snapshot, paths map[string]string) *Bridge {
	t.Helper()
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, Artifact: func(id string) (string, error) {
		p, ok := paths[id]
		if !ok {
			return "", errors.New("not owned")
		}
		return p, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.BaseURL = "bot", "owner", defaultBase
	b.state.Inputs["input"] = "accepted"
	freshWindow(b, "input")
	return b
}

func TestReplyTextAndDistinctSameNameArtifacts(t *testing.T) {
	f := newFileFixture(t)
	root := t.TempDir()
	paths := map[string]string{}
	for id, content := range map[string]string{"first": "one", "second": "two"} {
		path := filepath.Join(root, id+".json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		paths[id] = path
	}
	snapshot := fileSnapshot([]api.Artifact{{ID: "first", Name: "results.json"}, {ID: "second", Name: "results.json"}}, "Results attached")
	b := fileBridge(t, root, snapshot, paths)
	b.output(t.Context(), f.protocol())
	for _, id := range []string{"first", "second"} {
		if record := waitFile(t, b, id); record.State != "accepted" || record.MessageID != "18446744073709551615" {
			t.Fatalf("%s: %+v", id, record)
		}
	}
	f.mu.Lock()
	items := append([]messageItem(nil), f.items...)
	uploaded := make(map[string][]byte, len(f.uploaded))
	for k, v := range f.uploaded {
		uploaded[k] = v
	}
	f.mu.Unlock()
	if len(items) != 3 || items[0].Type != 1 || items[0].Text.Text != "Results attached" || b.state.Window.Used != 3 {
		t.Fatalf("text/files/budget: %#v %d", items, b.state.Window.Used)
	}
	seen := map[string]bool{}
	for _, item := range items[1:] {
		if item.Type != 4 || item.File == nil || item.File.Name != "results.json" || item.File.Len == "" || item.File.Media.Type != 1 {
			t.Fatalf("file item: %+v", item)
		}
		keyASCII, err := base64.StdEncoding.DecodeString(item.File.Media.AESKey)
		if err != nil || len(keyASCII) != 32 {
			t.Fatalf("media key: %v", err)
		}
		fileKey := item.File.Media.QueryParam[len("download-"):]
		if len(uploaded[fileKey]) != 3 || item.File.Len != "3" {
			t.Fatal("upload absent or wrong plaintext size")
		}
		seen[string(uploaded[fileKey])] = true
	}
	if !seen["one"] || !seen["two"] {
		t.Fatalf("wrong artifact bytes: %#v", seen)
	}
	b.output(t.Context(), f.protocol())
	reopened := fileBridge(t, root, snapshot, paths)
	reopened.output(t.Context(), f.protocol())
	f.mu.Lock()
	count := len(f.items)
	f.mu.Unlock()
	if count != 3 {
		t.Fatalf("snapshot/restart replay: %d", count)
	}
}

func TestArtifactOnlyBudgetAndFreshIngress(t *testing.T) {
	f := newFileFixture(t)
	root := t.TempDir()
	path := filepath.Join(root, "result.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := fileSnapshot([]api.Artifact{{ID: "artifact", Name: "result.json"}}, "")
	b := fileBridge(t, root, snapshot, map[string]string{"artifact": path})
	b.state.Window.Used = windowSendLimit
	b.output(t.Context(), f.protocol())
	if b.issue != "file_deferred" || b.state.Outputs[artifactKey("artifact")].State != "pending" {
		t.Fatalf("budget state: %s %+v", b.issue, b.state.Outputs)
	}
	// The source item may have paginated away before a new owner input. The
	// persisted handle is still safe to resolve and has not been sent yet.
	reopened := fileBridge(t, root, api.Snapshot{Connection: "ready"}, map[string]string{"artifact": path})
	freshWindow(reopened, "new-input")
	reopened.output(t.Context(), f.protocol())
	if record := waitFile(t, reopened, "artifact"); record.State != "accepted" || reopened.state.Window.Used != 1 {
		t.Fatalf("new ingress: %+v %+v", record, reopened.state.Window)
	}
	f.mu.Lock()
	count := len(f.items)
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("artifact only sends: %d", count)
	}
}

func TestOwnedCaelisToolArtifactWithoutAssistantText(t *testing.T) {
	f := newFileFixture(t)
	root := t.TempDir()
	path := filepath.Join(root, "result.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := fileSnapshot([]api.Artifact{{ID: "resource:session:id", Name: "result.json"}}, "")
	snapshot.Items[1].Kind = "tool"
	snapshot.Items[1].Text = "文件已生成"
	snapshot.Items = append(snapshot.Items, api.Item{ID: "activity", Kind: "activity", TurnKey: "turn", Status: "completed", Artifacts: []api.Artifact{{ID: "internal", Name: "private.txt"}}})
	b := fileBridge(t, root, snapshot, map[string]string{"resource:session:id": path})
	b.output(t.Context(), f.protocol())
	if record := waitFile(t, b, "resource:session:id"); record.State != "accepted" {
		t.Fatalf("resource file: %+v", record)
	}
	f.mu.Lock()
	items := append([]messageItem(nil), f.items...)
	f.mu.Unlock()
	if len(items) != 1 || items[0].Type != 4 {
		t.Fatalf("tool-only files: %+v", items)
	}
}

func TestConcurrentFilesLeaveDurableUnattemptedHandle(t *testing.T) {
	f := newFileFixture(t)
	f.uploadGate = make(chan struct{})
	root := t.TempDir()
	paths := map[string]string{}
	artifacts := []api.Artifact{}
	for _, id := range []string{"a", "b", "c"} {
		path := filepath.Join(root, id+".txt")
		if err := os.WriteFile(path, []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		paths[id] = path
		artifacts = append(artifacts, api.Artifact{ID: id, Name: id + ".txt"})
	}
	b := fileBridge(t, root, fileSnapshot(artifacts, ""), paths)
	b.output(t.Context(), f.protocol())
	if b.state.Outputs[artifactKey("c")].State != "pending" {
		t.Fatalf("third file not pending: %+v", b.state.Outputs[artifactKey("c")])
	}
	close(f.uploadGate)
	for _, id := range []string{"a", "b"} {
		if record := waitFile(t, b, id); record.State != "accepted" {
			t.Fatalf("%s: %+v", id, record)
		}
	}
	reopened := fileBridge(t, root, api.Snapshot{Connection: "ready"}, paths)
	reopened.output(t.Context(), f.protocol())
	if record := waitFile(t, reopened, "c"); record.State != "accepted" {
		t.Fatalf("pending file: %+v", record)
	}
	f.mu.Lock()
	count := len(f.items)
	f.mu.Unlock()
	if count != 3 {
		t.Fatalf("file sends: %d", count)
	}
}

func TestUnownedArtifactsNeverResolve(t *testing.T) {
	f := newFileFixture(t)
	read := 0
	snapshot := fileSnapshot([]api.Artifact{{ID: "private", Name: "private.txt"}}, "unrelated")
	snapshot.Items[0].RequestID = "desktop-input"
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }, Artifact: func(string) (string, error) { read++; return "", errors.New("unowned") }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	b.output(t.Context(), f.protocol())
	if read != 0 {
		t.Fatalf("resolved unrelated artifact %d times", read)
	}
}

func TestPreparingUploadCanResumeButCancelledPreparationNeverSends(t *testing.T) {
	f := newFileFixture(t)
	root := t.TempDir()
	path := filepath.Join(root, "safe.txt")
	if err := os.WriteFile(path, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := fileSnapshot([]api.Artifact{{ID: "safe", Name: "safe.txt"}}, "")
	b := fileBridge(t, root, snapshot, map[string]string{"safe": path})
	b.state.Outputs[artifactKey("safe")] = outbound{State: "preparing", Digest: textDigest("safe")}
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	reopened := fileBridge(t, root, snapshot, map[string]string{"safe": path})
	reopened.output(t.Context(), f.protocol())
	if record := waitFile(t, reopened, "safe"); record.State != "accepted" {
		t.Fatalf("resume: %+v", record)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, reason := f.protocol().uploadFile(ctx, "owner", []byte("no send")); reason != "upload_cancelled" {
		t.Fatal(reason)
	}
	f.mu.Lock()
	count := len(f.items)
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("cancelled upload sent: %d", count)
	}
}

func TestUploadAndSendFailuresDoNotReplay(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		uploadRet, uploadStatus, sendStatus, sendRet int
		want                                         string
		sends, uploads                               int
	}{
		{"upload URL rejected", -2, 0, 0, 0, "upload_rejected", 0, 0},
		{"CDN rejected", 0, 403, 0, 0, "upload_rejected", 0, 1},
		{"upload unknown", 0, 500, 0, 0, "upload_unknown", 0, 3},
		{"CDN redirect denied", 0, 302, 0, 0, "upload_unknown", 0, 3},
		{"send rejected", 0, 0, 0, -2, "rejected", 1, 1},
		{"send unknown", 0, 0, 500, 0, "unknown", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFileFixture(t)
			f.uploadRet = tc.uploadRet
			f.uploadStatus = tc.uploadStatus
			f.sendStatus = tc.sendStatus
			f.sendRet = tc.sendRet
			root := t.TempDir()
			path := filepath.Join(root, "data.bin")
			if err := os.WriteFile(path, []byte("bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			snapshot := fileSnapshot([]api.Artifact{{ID: "a", Name: "data.bin"}}, "")
			b := fileBridge(t, root, snapshot, map[string]string{"a": path})
			b.output(t.Context(), f.protocol())
			if record := waitFile(t, b, "a"); record.State != tc.want {
				t.Fatalf("state: %+v", record)
			}
			reopened := fileBridge(t, root, snapshot, map[string]string{"a": path})
			if reopened.Status().Issue == "" {
				t.Fatal("file failure disappeared after restart")
			}
			freshWindow(reopened, "new-input")
			reopened.output(t.Context(), f.protocol())
			f.mu.Lock()
			sends, uploads := len(f.items), f.uploads
			f.mu.Unlock()
			if sends != tc.sends || uploads != tc.uploads {
				t.Fatalf("replayed send or wrong upload attempts: sends=%d uploads=%d", sends, uploads)
			}
			state, err := os.ReadFile(filepath.Join(root, "weixin.json"))
			if err != nil || strings.Contains(string(state), "private server detail") || strings.Contains(string(state), "evil.test") {
				t.Fatalf("unsanitized state or unreadable state: %v", err)
			}
		})
	}
}

func TestFileSafetyAndCryptoFixture(t *testing.T) {
	key := make([]byte, 16)
	ciphertext, err := encryptFile([]byte("hello"), key)
	if err != nil || hex.EncodeToString(ciphertext) != "9834ed518cbc8fbe9af3c6ecb75eb8c0" {
		t.Fatalf("AES-128-ECB PKCS7 fixture: %x %v", ciphertext, err)
	}
	if got := string(decryptFixture(t, ciphertext, key)); got != "hello" {
		t.Fatal(got)
	}
	for _, name := range []string{"../secret", "", "evil\\path", "bad\nname"} {
		if fileName(name) {
			t.Fatalf("accepted name %q", name)
		}
	}
	for _, target := range []string{"http://novac2c.cdn.weixin.qq.com/c2c/upload", "https://novac2c.cdn.weixin.qq.com.evil.test/c2c/upload", "https://novac2c.cdn.weixin.qq.com/other", "https://user@novac2c.cdn.weixin.qq.com/c2c/upload"} {
		p := &protocol{cdnBase: "https://novac2c.cdn.weixin.qq.com/c2c"}
		if _, err := p.uploadURL(uploadURLResult{FullURL: target}, "key"); err == nil {
			t.Fatalf("accepted CDN %s", target)
		}
	}
	p := &protocol{cdnBase: "https://novac2c.cdn.weixin.qq.com/c2c"}
	if target, err := p.uploadURL(uploadURLResult{Param: "a&b"}, "key"); err != nil || target != "https://novac2c.cdn.weixin.qq.com/c2c/upload?encrypted_query_param=a%26b&filekey=key" {
		t.Fatalf("fallback URL: %s %v", target, err)
	}
}
