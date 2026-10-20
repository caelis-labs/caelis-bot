package managedupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

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
func TestSignaturePlatformAndTestTrustBoundaries(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	o, _ := testPackage(t, key, "0.17.1-qa.2", nil)
	if _, e := Verify(o.Bytes, o.Signature, pub, "linux-arm64", false); e == nil {
		t.Fatal("QA accepted by release verifier")
	}
	if _, e := Verify(o.Bytes, o.Signature, pub, "linux-amd64", true); e == nil {
		t.Fatal("wrong architecture accepted")
	}
	bad := bytes.Clone(o.Bytes)
	bad[0] = ' '
	if _, e := Verify(bad, o.Signature, pub, "linux-arm64", true); e == nil {
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
