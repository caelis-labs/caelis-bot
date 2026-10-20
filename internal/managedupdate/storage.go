package managedupdate

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type Installation struct {
	Schema  int      `json:"schema"`
	Profile string   `json:"profile"`
	Args    []string `json:"args"`
	QA      bool     `json:"qa"`
}
type selection struct {
	Release string `json:"release"`
}

func readJSON(path string, out any) error {
	if e := privateFile(path); e != nil {
		return e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if len(b) > 4<<20 || json.Unmarshal(b, out) != nil {
		return problem("state_invalid")
	}
	return nil
}
func writeJSON(path string, value any) error { return localstate.WriteConfirmed(path, value) }
func selected(root string) (string, error) {
	var s selection
	if e := readJSON(filepath.Join(root, "current.json"), &s); e != nil {
		return "", e
	}
	if !hexID.MatchString(s.Release) {
		return "", problem("selection_invalid")
	}
	return s.Release, nil
}
func selectRelease(root, id string) error {
	if !hexID.MatchString(id) {
		return problem("selection_invalid")
	}
	return writeJSON(filepath.Join(root, "current.json"), selection{id})
}
func loadOffer(root, id string, t Trust, platform string) (Offer, error) {
	var empty Offer
	if !hexID.MatchString(id) {
		return empty, problem("offer_invalid")
	}
	dir := filepath.Join(root, "versions", id)
	if e := privateDirectory(dir); e != nil {
		return empty, e
	}
	data, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return empty, e
	}
	sig, e := os.ReadFile(filepath.Join(dir, "manifest.sig"))
	if e != nil {
		return empty, e
	}
	o, e := Verify(data, sig, t.Key, platform, t.QA)
	if e != nil {
		return empty, e
	}
	if o.ID != id {
		return empty, problem("offer_identity_mismatch")
	}
	for _, f := range o.Manifest.Members {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if e = verifyFile(p, f.Length, f.SHA256); e != nil {
			return empty, e
		}
		i, e := os.Lstat(p)
		if e != nil || int64(i.Mode().Perm()) != f.Mode {
			return empty, problem("member_mode_mismatch")
		}
	}
	return o, verifyRuntimePin(dir, o.Manifest)
}
func verifyRuntimePin(dir string, m Manifest) error {
	b, e := os.ReadFile(filepath.Join(dir, "Resources", "Caelis-Runtime.json"))
	if e != nil {
		return e
	}
	var marker struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Targets map[string]struct {
			Binary string `json:"binary_sha256"`
		} `json:"targets"`
	}
	if json.Unmarshal(b, &marker) != nil || marker.Version != m.Runtime.Version || marker.Commit != m.Runtime.Source ||
		marker.Targets[m.Platform].Binary != m.Runtime.SHA256 {
		return problem("runtime_pin_mismatch")
	}
	for _, name := range []string{"caelisbot", "Helpers/caelis"} {
		f, e := os.Open(filepath.Join(dir, name))
		if e != nil {
			return e
		}
		var head [20]byte
		_, e = io.ReadFull(f, head[:])
		f.Close()
		machine := byte(183)
		if m.Platform == "linux-amd64" {
			machine = 62
		}
		if e != nil || string(head[:6]) != "\x7fELF\x02\x01" || head[18] != machine || head[19] != 0 {
			return problem("executable_platform_mismatch")
		}
	}
	return nil
}
func stage(root, archive string, o Offer, t Trust, platform string) (string, error) {
	if _, e := Verify(o.Bytes, o.Signature, t.Key, platform, t.QA); e != nil {
		return "", e
	}
	if e := verifyFile(archive, o.Manifest.Length, o.Manifest.SHA256); e != nil {
		return "", e
	}
	versions := filepath.Join(root, "versions")
	if e := os.MkdirAll(versions, 0700); e != nil {
		return "", e
	}
	if e := privateDirectory(versions); e != nil {
		return "", e
	}
	if _, e := loadOffer(root, o.ID, t, platform); e == nil {
		return o.ID, nil
	}
	if _, e := os.Lstat(filepath.Join(versions, o.ID)); !os.IsNotExist(e) {
		return "", problem("staging_conflict")
	}
	if e := diskBudget(versions, o.Manifest.ExpandedBytes); e != nil {
		return "", e
	}
	dir, e := os.MkdirTemp(versions, ".stage-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(dir)
	f, e := os.Open(archive)
	if e != nil {
		return "", e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return "", e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	expected := map[string]Member{}
	for _, f := range o.Manifest.Members {
		expected[f.Path] = f
	}
	seen := map[string]bool{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
		member, ok := expected[h.Name]
		if !ok || seen[h.Name] || h.Typeflag != tar.TypeReg || h.Size != member.Length ||
			h.Mode != member.Mode || h.Linkname != "" {
			return "", problem("archive_member_invalid")
		}
		seen[h.Name] = true
		target := filepath.Join(dir, filepath.FromSlash(h.Name))
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return "", e
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(member.Mode))
		if e != nil {
			return "", e
		}
		_, e = io.Copy(out, tr)
		// The service deliberately runs with umask 0077. Restore signed file
		// modes inside the private staging directory, never alter process umask.
		if e == nil {
			e = out.Chmod(os.FileMode(member.Mode))
			if e == nil {
				info, statErr := out.Stat()
				e = statErr
				if e == nil && int64(info.Mode().Perm()) != member.Mode {
					e = problem("member_mode_mismatch")
				}
			}
		}
		if e == nil {
			e = out.Sync()
		}
		ce := out.Close()
		if e != nil {
			return "", e
		}
		if ce != nil {
			return "", ce
		}
		if e = verifyFile(target, member.Length, member.SHA256); e != nil {
			return "", e
		}
		if e = localstate.SyncParent(target); e != nil {
			return "", e
		}
	}
	if len(seen) != len(expected) {
		return "", problem("archive_incomplete")
	}
	// Consume gzip trailer; a truncated stream must not become a ready release.
	// Standard tar writers may pad to a 10 KiB block. Bound and require zero
	// padding, then consume EOF to verify the gzip checksum/trailer.
	tail, e := io.ReadAll(io.LimitReader(gz, 10241))
	if e != nil {
		return "", e
	}
	if len(tail) > 10240 || strings.Trim(string(tail), "\x00") != "" {
		return "", problem("archive_trailer_invalid")
	}
	if e = verifyRuntimePin(dir, o.Manifest); e != nil {
		return "", e
	}
	for name, b := range map[string][]byte{"manifest.json": o.Bytes, "manifest.sig": o.Signature} {
		file, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		_, e = file.Write(b)
		if e == nil {
			e = file.Sync()
		}
		ce := file.Close()
		if e != nil {
			return "", e
		}
		if ce != nil {
			return "", ce
		}
	}
	if e = localstate.SyncParent(filepath.Join(dir, "manifest.json")); e != nil {
		return "", e
	}
	if e = os.Rename(dir, filepath.Join(versions, o.ID)); e != nil {
		return "", e
	}
	return o.ID, localstate.SyncParent(filepath.Join(versions, o.ID))
}

// Initialize is an explicit local installation command. It refuses an existing
// root, data in the code tree, and unverified bytes. It never starts the Bot.
func Initialize(root, profile, archive string, o Offer, t Trust, platform string, args []string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(profile) || root == profile {
		return problem("installation_paths_invalid")
	}
	if rel, e := filepath.Rel(root, profile); e != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return problem("profile_must_be_external")
	}
	if e := os.Mkdir(root, 0700); e != nil {
		return e
	}
	if e := privateDirectory(root); e != nil {
		return e
	}
	// Leave failed initializations visible; never erase unknown preexisting data.
	if _, e := stage(root, archive, o, t, platform); e != nil {
		return e
	}
	if e := os.MkdirAll(profile, 0700); e != nil {
		return e
	}
	if e := privateDirectory(profile); e != nil {
		return e
	}
	if e := writeJSON(filepath.Join(root, "installation.json"), Installation{1, profile, args, t.QA}); e != nil {
		return e
	}
	return selectRelease(root, o.ID)
}

// A selected version must not silently become an unmanaged second profile when
// invoked directly. Only the launcher supplies the inherited installation lease
// and the exact-child health acknowledgement. Copying an independent bundle is a
// separate, unmanaged installation outside this owner's filesystem namespace.
func RequiresLauncher(executable string) bool {
	version := filepath.Dir(executable)
	return filepath.Base(executable) == "caelisbot" && hexID.MatchString(filepath.Base(version)) && filepath.Base(filepath.Dir(version)) == "versions"
}
