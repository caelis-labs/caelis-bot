package weixin

import (
	"context"
	"crypto/aes"
	"crypto/md5"
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
	return waitFileTurn(t, b, "turn", id)
}

func waitFileTurn(t *testing.T, b *Bridge, turn, id string) outbound {
	t.Helper()
	// Wait for the original upload/send operation, not its pre-HTTP unknown intent.
	b.fileWait.Wait()
	deadline := time.After(4 * time.Second)
	for {
		b.mu.Lock()
		record := b.state.Outputs[artifactKey(turn, id)]
		active := b.fileWork[artifactKey(turn, id)]
		b.mu.Unlock()
		if record.State != "" && record.State != "pending" && record.State != "preparing" && !active {
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
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }, Artifact: func(id string) (string, error) {
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

func TestPreparingUploadCanResumeButCancelledPreparationNeverSends(t *testing.T) {
	f := newFileFixture(t)
	root := t.TempDir()
	path := filepath.Join(root, "safe.txt")
	if err := os.WriteFile(path, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := fileSnapshot([]api.Artifact{{ID: "safe", Name: "safe.txt"}}, "")
	b := fileBridge(t, root, snapshot, map[string]string{"safe": path})
	b.state.Outputs[artifactKey("turn", "safe")] = outbound{State: "preparing", ArtifactID: "safe", FileTurnKey: "turn", FileName: "safe.txt", FileInputID: "input", Digest: textDigest("safe")}
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

func TestPreparingFileCannotBorrowNextInputWindow(t *testing.T) {
	f := newFileFixture(t)
	f.uploadGate = make(chan struct{})
	root := t.TempDir()
	path := filepath.Join(root, "safe.txt")
	if err := os.WriteFile(path, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	b := fileBridge(t, root, fileSnapshot([]api.Artifact{{ID: "safe", Name: "safe.txt"}}, ""), map[string]string{"safe": path})
	b.output(t.Context(), f.protocol())
	deadline := time.After(4 * time.Second)
	for {
		f.mu.Lock()
		started := f.uploads > 0
		f.mu.Unlock()
		if started {
			break
		}
		select {
		case <-deadline:
			t.Fatal("upload did not start")
		case <-time.After(5 * time.Millisecond):
		}
	}
	b.mu.Lock()
	freshWindow(b, "new-input")
	b.mu.Unlock()
	close(f.uploadGate)
	if record := waitFile(t, b, "safe"); record.State != "deferred" || record.Attempts != 0 {
		t.Fatalf("borrowed new quota: %+v", record)
	}
	f.mu.Lock()
	sends := len(f.items)
	f.mu.Unlock()
	if sends != 0 || b.state.Window.Used != 0 {
		t.Fatalf("old file sent on new input: %d %+v", sends, b.state.Window)
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
