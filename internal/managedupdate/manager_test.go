package managedupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	trust         Trust
	key           ed25519.PrivateKey
	root, profile string
	a, b          Offer
}

func testPackage(t *testing.T, key ed25519.PrivateKey, version string, edit func(map[string][]byte)) (Offer, []byte) {
	t.Helper()
	elf := append([]byte("\x7fELF\x02\x01"), make([]byte, 14)...)
	elf[18] = 183
	bot := append(bytes.Clone(elf), []byte(version)...)
	runtime := append(bytes.Clone(elf), []byte("fixed-runtime")...)
	marker, _ := json.Marshal(map[string]any{"version": "v0.72.2", "commit": strings.Repeat("b", 40), "targets": map[string]any{"linux-arm64": map[string]string{"binary_sha256": hash(runtime)}}})
	files := map[string][]byte{"caelisbot": bot, "Helpers/caelis": runtime, "Resources/Caelis-Runtime.json": marker, "Resources/Caelis-Runtime-LICENSE": []byte("runtime license"), "LICENSE": []byte("license")}
	for _, name := range []string{"ASSET-LICENSE.md", "Resources/Caelis-Protocol-LICENSE", "Resources/OpenClaw-Weixin-LICENSE", "Resources/Go-QRCode-LICENSE"} {
		files[name] = []byte("license fixture")
	}
	if edit != nil {
		edit(files)
	}
	m := Manifest{Schema: 1, Product: "caelisbot", Flavor: "bundled", Platform: "linux-arm64", Channel: "qa", Version: version, Source: strings.Repeat("a", 40), BuildKind: "qa", Launcher: 1, StateSchema: 1, Runtime: RuntimePin{"v0.72.2", strings.Repeat("b", 40), hash(runtime)}}
	var pin struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(files["Resources/Caelis-Runtime.json"], &pin); err != nil {
		t.Fatal(err)
	}
	m.Runtime = RuntimePin{pin.Version, pin.Commit, hash(files["Helpers/caelis"])}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tarw := tar.NewWriter(gz)
	for path, body := range files {
		mode := int64(0644)
		if path == "caelisbot" || path == "Helpers/caelis" {
			mode = 0755
		}
		m.Members = append(m.Members, Member{path, hash(body), int64(len(body)), mode})
		m.ExpandedBytes += int64(len(body))
		if e := tarw.WriteHeader(&tar.Header{Name: path, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		if _, e := tarw.Write(body); e != nil {
			t.Fatal(e)
		}
	}
	if e := tarw.Close(); e != nil {
		t.Fatal(e)
	}
	if e := gz.Close(); e != nil {
		t.Fatal(e)
	}
	m.File = "caelisbot-" + version + "-linux-arm64.tar.gz"
	m.Length = int64(archive.Len())
	m.SHA256 = hash(archive.Bytes())
	data, _ := json.Marshal(m)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
	o, e := Verify(data, sig, key.Public().(ed25519.PublicKey), "linux-arm64", true)
	if e != nil {
		t.Fatal(e)
	}
	return o, archive.Bytes()
}
func fixtureManager(t *testing.T) (*Manager, fixture) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	base := t.TempDir()
	resolved, e := filepath.EvalSymlinks(base)
	if e != nil {
		t.Fatal(e)
	}
	f := fixture{trust: Trust{Key: pub, QA: true}, key: key, root: filepath.Join(resolved, "install"), profile: filepath.Join(resolved, "profile")}
	a, archive := testPackage(t, key, "0.17.1-qa.1", nil)
	f.a = a
	path := filepath.Join(resolved, "a.tar.gz")
	if e = os.WriteFile(path, archive, 0600); e != nil {
		t.Fatal(e)
	}
	if e = Initialize(f.root, f.profile, path, a, f.trust, "linux-arm64", nil); e != nil {
		t.Fatal(e)
	}
	b, archive := testPackage(t, key, "0.17.1-qa.2", nil)
	f.b = b
	path = filepath.Join(resolved, "b.tar.gz")
	if e = os.WriteFile(path, archive, 0600); e != nil {
		t.Fatal(e)
	}
	m, e := Open(t.Context(), f.root, "linux-arm64", f.trust)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	if _, e = stage(f.root, path, b, f.trust, "linux-arm64"); e != nil {
		t.Fatal(e)
	}
	m.state.Offer = &b
	m.state.Status.State = "ready"
	m.state.Status.OfferID = b.ID
	m.state.Status.Automatic = false
	if e = m.save(); e != nil {
		t.Fatal(e)
	}
	return m, f
}
func TestSignaturePlatformAndTestTrustBoundaries(t *testing.T) {
	_, f := fixtureManager(t)
	if _, e := Verify(f.b.Bytes, f.b.Signature, f.trust.Key, "linux-arm64", false); e == nil {
		t.Fatal("QA accepted by release verifier")
	}
	if _, e := Verify(f.b.Bytes, f.b.Signature, f.trust.Key, "linux-amd64", true); e == nil {
		t.Fatal("wrong architecture accepted")
	}
	bad := bytes.Clone(f.b.Bytes)
	bad[0] = ' '
	if _, e := Verify(bad, f.b.Signature, f.trust.Key, "linux-arm64", true); e == nil {
		t.Fatal("modified manifest accepted")
	}
	if !qaBuild {
		if _, e := ReadQATrust("/unused"); Code(e) != "test_trust_disabled" {
			t.Fatal(e)
		}
	}
	if len(ProductionTrust().Key) != 0 {
		t.Log("Reviewed production key configured; QA key must remain independent")
	}
}
func TestInstallConcurrentTabsAndOriginalReceipt(t *testing.T) {
	m, f := fixtureManager(t)
	var wg sync.WaitGroup
	results := make(chan Receipt, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := m.Command(Command{ID: "original_install_12345", Action: "install", OfferID: f.b.ID, When: "idle"})
			if e != nil {
				t.Error(e)
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	var id string
	for r := range results {
		if id == "" {
			id = r.TransactionID
		}
		if r.TransactionID != id || r.State != "accepted" {
			t.Fatal(r)
		}
	}
	r, e := m.Command(Command{ID: "another_tab_12345678", Action: "install", OfferID: f.b.ID, When: "now"})
	if e != nil || r.TransactionID != id {
		t.Fatal("multiple transactions", r, e)
	}
	if _, e = m.Command(Command{ID: "original_install_12345", Action: "check"}); Code(e) != "request_conflict" {
		t.Fatal(e)
	}
	reopened, e := Open(t.Context(), f.root, "linux-arm64", f.trust)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	got, e := reopened.Receipt("original_install_12345")
	if e != nil || got.TransactionID != id {
		t.Fatal(got, e)
	}
}
func TestCancellationFencesLateShutdownCommit(t *testing.T) {
	m, f := fixtureManager(t)
	r, e := m.Command(Command{ID: "install_for_cancel_123", Action: "install", OfferID: f.b.ID, When: "idle"})
	if e != nil {
		t.Fatal(e)
	}
	m.boot = Boot{ID: "original-boot", PID: 123, Identity: f.a.Manifest.Identity()}
	m.ready = true
	h, e := m.Handoff(m.boot.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.Command(Command{ID: "cancel_for_test_12345", Action: "cancel", TransactionID: r.TransactionID, Revision: m.Status().Revision})
	if e != nil {
		t.Fatal(e)
	}
	h.State = "committed"
	if e = m.FinishHandoff(h); Code(e) != "handoff_conflict" {
		t.Fatal("late commit revived install", e)
	}
	id, e := selected(f.root)
	if e != nil || id != f.a.ID {
		t.Fatal("selection changed", e)
	}
}
func TestCrashRecoveryUsesCommitAndHealth(t *testing.T) {
	for _, phase := range []string{"waiting", "requested", "committed", "switching", "switched", "rolling_back"} {
		t.Run(phase, func(t *testing.T) {
			m, f := fixtureManager(t)
			m.state.Transaction = &transaction{ID: "original-transaction", Offer: f.b.ID, Previous: f.a.ID, Phase: phase, When: "idle"}
			if phase == "switched" || phase == "rolling_back" {
				if e := selectRelease(f.root, f.b.ID); e != nil {
					t.Fatal(e)
				}
			}
			if e := m.save(); e != nil {
				t.Fatal(e)
			}
			n, e := Open(t.Context(), f.root, "linux-arm64", f.trust)
			if e != nil {
				t.Fatal(e)
			}
			defer n.Close()
			id, e := selected(f.root)
			if e != nil {
				t.Fatal(e)
			}
			want := f.a.ID
			if phase == "committed" || phase == "switching" {
				want = f.b.ID
			}
			if id != want {
				t.Fatal("wrong selection", id, want)
			}
			if n.Status().State == "installed" {
				t.Fatal("crash recovery invented health")
			}
			selectedOffer := f.a
			if want == f.b.ID {
				selectedOffer = f.b
			}
			n.boot = Boot{ID: "new-boot", PID: 321, Identity: selectedOffer.Manifest.Identity()}
			spoof := n.boot
			spoof.Identity.Source = strings.Repeat("c", 40)
			if e = n.Ready(spoof); e == nil {
				t.Fatal("wrong process build accepted")
			}
			if e = n.Ready(n.boot); e != nil {
				t.Fatal(e)
			}
			if n.state.Transaction.ID != "original-transaction" {
				t.Fatal("transaction replaced")
			}
		})
	}
}
func TestNoCompatiblePublishedAssetAndProgress(t *testing.T) {
	_, f := fixtureManager(t)
	offer, archive := testPackage(t, f.key, "0.17.1-qa.3", nil)
	var address string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			_, _ = w.Write([]byte(`[{"tag_name":"v0.18.0","assets":[{"name":"mac.dmg"}]}]`))
			return
		}
		if r.URL.Path == "/releases" {
			var assets []map[string]string
			for _, name := range []string{"linux-arm64-update.json", "linux-arm64-update.json.sig", offer.Manifest.File} {
				assets = append(assets, map[string]string{"name": name, "browser_download_url": address + "/releases/download/v" + offer.Manifest.Version + "/" + name})
			}
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"tag_name": "v" + offer.Manifest.Version, "assets": assets}})
			return
		}
		switch filepath.Base(r.URL.Path) {
		case "linux-arm64-update.json":
			_, _ = w.Write(offer.Bytes)
		case "linux-arm64-update.json.sig":
			_, _ = w.Write(offer.Signature)
		default:
			for i := 0; i < len(archive); i += 31 {
				end := min(i+31, len(archive))
				_, _ = w.Write(archive[i:end])
				w.(http.Flusher).Flush()
				time.Sleep(time.Millisecond)
			}
		}
	}))
	defer server.Close()
	address = server.URL
	trust := Trust{QA: true, Key: f.trust.Key, Base: address + "/releases/download", Feed: address + "/empty"}
	o, e := trust.Check(t.Context(), "0.17.1-qa.1", "linux-arm64")
	if e != nil || o != nil {
		t.Fatal("invented Linux asset", o, e)
	}
	trust.Feed = address + "/releases"
	o, e = trust.Check(t.Context(), "0.17.1-qa.1", "linux-arm64")
	if e != nil || o == nil {
		t.Fatal(e)
	}
	var counts []int64
	downloadDir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	file, e := trust.Download(t.Context(), *o, filepath.Join(downloadDir, "download"), func(n int64) { counts = append(counts, n) })
	if e != nil {
		t.Fatal(e)
	}
	if len(counts) < 2 || counts[len(counts)-1] != int64(len(archive)) {
		t.Fatal("not actual incremental bytes", counts)
	}
	if e = verifyFile(file, o.Manifest.Length, o.Manifest.SHA256); e != nil {
		t.Fatal(e)
	}
}
func TestForeignRootAndUnsafeArchiveFailBeforeSelection(t *testing.T) {
	m, f := fixtureManager(t)
	link := filepath.Join(filepath.Dir(f.root), "linked")
	if e := os.Symlink(f.root, link); e != nil {
		t.Fatal(e)
	}
	if _, e := lockDirectory(link); e == nil {
		t.Fatal("symlink root accepted")
	}
	unlock, e := lockDirectory(f.root)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if _, e = lockDirectory(f.root); Code(e) != "installation_owned" {
		t.Fatal(e)
	}
	before := m.Status()
	bad := f.b
	bad.Manifest.Members[0].Path = "../escape"
	if e = bad.Manifest.Validate("linux-arm64", true); e == nil {
		t.Fatal("traversal accepted")
	}
	if !reflect.DeepEqual(before, m.Status()) {
		t.Fatal("preflight mutated owner")
	}
	if e = diskBudget(f.root, 1<<62); Code(e) != "disk_budget" {
		t.Fatal(e)
	}
}

func TestReceiptPersistenceFailureDoesNotAuthorizeHandoff(t *testing.T) {
	m, f := fixtureManager(t)
	if e := os.Remove(filepath.Join(f.root, "update.json")); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(f.root, "update.json"), 0700); e != nil {
		t.Fatal(e)
	}
	c := Command{ID: "lost_persistence_123456", Action: "install", OfferID: f.b.ID, When: "idle"}
	if _, e := m.Command(c); e == nil {
		t.Fatal("lost durability reported success")
	}
	if _, e := m.Command(c); Code(e) != "receipt_unconfirmed" {
		t.Fatal(e)
	}
	if _, e := m.Receipt(c.ID); Code(e) != "receipt_unconfirmed" {
		t.Fatal(e)
	}
	m.boot = Boot{ID: "original-boot", PID: 123, Identity: f.a.Manifest.Identity()}
	m.ready = true
	if _, e := m.Handoff(m.boot.ID); Code(e) != "update_unconfirmed" {
		t.Fatal("unknown write authorized handoff", e)
	}
	id, _ := selected(f.root)
	if id != f.a.ID {
		t.Fatal("old selected code changed")
	}
}
func TestAlreadyRunningOfferCannotReinstall(t *testing.T) {
	m, f := fixtureManager(t)
	m.state.Status.Current = f.b.Manifest.Identity()
	m.state.Status.State = "installed"
	if _, e := m.Command(Command{ID: "stale_offer_123456789", Action: "download", OfferID: f.b.ID}); Code(e) != "offer_not_newer" {
		t.Fatal(e)
	}
}

func TestCorruptUnacknowledgedCandidateRecoversPrevious(t *testing.T) {
	m, f := fixtureManager(t)
	m.state.Transaction = &transaction{ID: "original-recovery", Offer: f.b.ID, Previous: f.a.ID, Phase: "switched"}
	if e := selectRelease(f.root, f.b.ID); e != nil {
		t.Fatal(e)
	}
	if e := m.save(); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(f.root, "versions", f.b.ID, "caelisbot"), []byte("corrupt"), 0755); e != nil {
		t.Fatal(e)
	}
	n, e := Open(t.Context(), f.root, "linux-arm64", f.trust)
	if e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	id, _ := selected(f.root)
	if id != f.a.ID || n.Status().State != "rolling_back" || n.Status().Current != f.a.Manifest.Identity() {
		t.Fatal(n.Status())
	}
}
func TestUnavailableRollbackPersistsUnknown(t *testing.T) {
	m, f := fixtureManager(t)
	m.state.Transaction = &transaction{ID: "original-recovery", Offer: f.b.ID, Previous: f.a.ID, Phase: "switched"}
	if e := selectRelease(f.root, f.b.ID); e != nil {
		t.Fatal(e)
	}
	if e := m.save(); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(f.root, "versions", f.a.ID, "caelisbot")); e != nil {
		t.Fatal(e)
	}
	if n, e := Open(t.Context(), f.root, "linux-arm64", f.trust); Code(e) != "rollback_unavailable" {
		if n != nil {
			n.Close()
		}
		t.Fatal(e)
	}
	var disk journal
	if e := readJSON(filepath.Join(f.root, "update.json"), &disk); e != nil {
		t.Fatal(e)
	}
	if disk.Transaction.ID != "original-recovery" || disk.Transaction.Phase != "unknown" || disk.Status.State != "unknown" {
		t.Fatal(disk.Status)
	}
}

func TestManagedVersionCannotBecomeAnotherUnmanagedProfile(t *testing.T) {
	if !RequiresLauncher("/private/install/versions/" + strings.Repeat("a", 64) + "/caelisbot") {
		t.Fatal("managed namespace not protected")
	}
	if RequiresLauncher("/private/standalone/caelisbot") {
		t.Fatal("independent installation rejected")
	}
}
