package managedupdate

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/updates"
)

const (
	MaxArchive  = 512 << 20
	MaxExpanded = 1 << 30
	MaxFiles    = 64
)

type Member struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Length int64  `json:"length"`
	Mode   int64  `json:"mode"`
}
type RuntimePin struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	SHA256  string `json:"sha256"`
}
type Manifest struct {
	Schema        int        `json:"schema"`
	Product       string     `json:"product"`
	Flavor        string     `json:"flavor"`
	Platform      string     `json:"platform"`
	Channel       string     `json:"channel"`
	Version       string     `json:"version"`
	Source        string     `json:"source"`
	BuildKind     string     `json:"buildKind"`
	Launcher      int        `json:"launcher"`
	StateSchema   int        `json:"stateSchema"`
	File          string     `json:"file"`
	SHA256        string     `json:"sha256"`
	Length        int64      `json:"length"`
	ExpandedBytes int64      `json:"expandedBytes"`
	Runtime       RuntimePin `json:"runtime"`
	Members       []Member   `json:"members"`
}
type Offer struct {
	ID        string   `json:"id"`
	Manifest  Manifest `json:"manifest"`
	Bytes     []byte   `json:"bytes"`
	Signature []byte   `json:"signature"`
}

var hexID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sourceID = regexp.MustCompile(`^[a-f0-9]{40}$`)
var ingressID = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,128}$`)

func hash(b []byte) string            { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (m Manifest) Identity() Identity { return Identity{m.Version, m.Source, m.BuildKind} }
func (m Manifest) Validate(platform string, qa bool) error {
	if m.Schema != 1 || m.Product != "caelisbot" || m.Flavor != "bundled" ||
		m.Platform != platform || (platform != "linux-arm64" && platform != "linux-amd64") ||
		m.Launcher != Protocol || m.StateSchema != 1 || !sourceID.MatchString(m.Source) ||
		m.Length <= 0 || m.Length > MaxArchive || !hexID.MatchString(m.SHA256) ||
		m.ExpandedBytes <= 0 || m.ExpandedBytes > MaxExpanded ||
		len(m.Members) < 4 || len(m.Members) > MaxFiles {
		return problem("invalid_manifest")
	}
	if _, err := updates.CompareVersions(m.Version, m.Version); err != nil {
		return problem("invalid_version")
	}
	if qa {
		if m.Channel != "qa" || m.BuildKind != "qa" || !strings.Contains(m.Version, "-qa.") {
			return problem("test_artifact_required")
		}
	} else if m.Channel != "stable" || m.BuildKind != "release" || strings.Contains(m.Version, "-") {
		return problem("release_artifact_required")
	}
	if m.File != "caelisbot-"+m.Version+"-"+platform+".tar.gz" ||
		!sourceID.MatchString(m.Runtime.Source) || !hexID.MatchString(m.Runtime.SHA256) ||
		m.Runtime.Version == "" {
		return problem("invalid_artifact")
	}
	seen := map[string]Member{}
	var total int64
	for _, f := range m.Members {
		if f.Path == "." || path.Clean(f.Path) != f.Path || strings.HasPrefix(f.Path, "/") ||
			strings.Contains(f.Path, "\\") || strings.Contains(f.Path, ":") ||
			strings.HasPrefix(f.Path, "../") || len(f.Path) > 160 ||
			f.Length <= 0 || f.Length > MaxExpanded || !hexID.MatchString(f.SHA256) ||
			(f.Mode != 0644 && f.Mode != 0755) {
			return problem("invalid_member")
		}
		if _, ok := seen[f.Path]; ok {
			return problem("duplicate_member")
		}
		// No writable code/config hooks or executable surprises in this flavor.
		switch f.Path {
		case "caelisbot", "Helpers/caelis":
			if f.Mode != 0755 {
				return problem("invalid_member_mode")
			}
		case "Resources/Caelis-Runtime.json", "Resources/Caelis-Runtime-LICENSE", "LICENSE", "README.md", "ASSET-LICENSE.md", "Resources/Caelis-Protocol-LICENSE", "Resources/OpenClaw-Weixin-LICENSE", "Resources/Go-QRCode-LICENSE":
			if f.Mode != 0644 {
				return problem("invalid_member_mode")
			}
		default:
			return problem("unsupported_member")
		}
		seen[f.Path] = f
		total += f.Length
	}
	for _, name := range []string{"caelisbot", "Helpers/caelis", "Resources/Caelis-Runtime.json", "Resources/Caelis-Runtime-LICENSE", "LICENSE", "ASSET-LICENSE.md", "Resources/Caelis-Protocol-LICENSE", "Resources/OpenClaw-Weixin-LICENSE", "Resources/Go-QRCode-LICENSE"} {
		if _, ok := seen[name]; !ok {
			return problem("missing_member")
		}
	}
	if total != m.ExpandedBytes || seen["Helpers/caelis"].SHA256 != m.Runtime.SHA256 {
		return problem("runtime_pin_mismatch")
	}
	return nil
}

func Verify(data, signature []byte, key ed25519.PublicKey, platform string, qa bool) (Offer, error) {
	var o Offer
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || len(data) > 65536 || len(key) != ed25519.PublicKeySize ||
		!ed25519.Verify(key, data, sig) {
		return o, problem("signature_invalid")
	}
	if json.Unmarshal(data, &o.Manifest) != nil {
		return o, problem("invalid_manifest")
	}
	if err = o.Manifest.Validate(platform, qa); err != nil {
		return o, err
	}
	o.ID, o.Bytes, o.Signature = hash(data), data, signature
	return o, nil
}

func verifyFile(file string, length int64, digest string) error {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() != length {
		return problem("artifact_invalid")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, length+1))
	if err != nil || n != length || hex.EncodeToString(h.Sum(nil)) != digest {
		return problem("digest_mismatch")
	}
	return nil
}
