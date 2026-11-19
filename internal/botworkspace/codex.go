package botworkspace

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const codexConfig = ".codex/config.toml"
const codexRollbackCopy = ".codex/.caelis-bot-upgrade.toml"
const legacyPluginMarker = "# Managed by Caelis Bot. Bot workspace only.\n"

var pluginServerID = regexp.MustCompile(`^p_[a-f0-9]{8}_`)

func decodeTOML(body []byte) (map[string]any, error) {
	var doc map[string]any
	if _, err := toml.Decode(string(body), &doc); err != nil {
		return nil, errors.New("invalid Codex project configuration")
	}
	return doc, nil
}

func table(doc map[string]any) (map[string]any, error) {
	value, ok := doc["mcp_servers"]
	if !ok {
		return map[string]any{}, nil
	}
	if servers, ok := value.(map[string]any); ok {
		return servers, nil
	}
	return nil, errors.New("invalid Codex project MCP server map")
}

const pluginBlockStart = "# BEGIN Caelis Bot plugins"
const pluginBlockEnd = "# END Caelis Bot plugins"

func splitPluginBlock(body []byte) (base, owned []byte, err error) {
	var quote string
	var offset, start, content, end int
	for raw := range strings.SplitAfterSeq(string(body), "\n") {
		line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		next := offset + len(raw)
		if quote == "" {
			switch line {
			case pluginBlockStart:
				if content != 0 {
					return nil, nil, errors.New("duplicate managed Codex configuration block")
				}
				start, content = offset, next
			case pluginBlockEnd:
				if content == 0 || end != 0 {
					return nil, nil, errors.New("invalid managed Codex configuration block")
				}
				owned, end = body[content:offset], next
			}
		}
		quote = tomlLineQuote(line, quote)
		offset = next
	}
	if content == 0 {
		return body, nil, nil
	}
	if end == 0 {
		return nil, nil, errors.New("unterminated managed Codex configuration block")
	}
	return append(bytes.Clone(body[:start]), body[end:]...), owned, nil
}

// The document has already passed native TOML parsing. Track only string and
// comment boundaries here to distinguish marker comments from string content.
func tomlLineQuote(line, quote string) string {
	for len(line) > 0 {
		if quote == "" {
			i := strings.IndexAny(line, "#\"'")
			if i < 0 || line[i] == '#' {
				break
			}
			line = line[i:]
			quote = line[:1]
			if len(line) >= 3 && line[1] == line[0] && line[2] == line[0] {
				quote = line[:3]
			}
			line = line[len(quote):]
			continue
		}
		if quote[0] == '"' && line[0] == '\\' && len(line) > 1 {
			line = line[2:]
			continue
		}
		if strings.HasPrefix(line, quote) {
			line = line[len(quote):]
			if len(quote) == 3 {
				// TOML permits one or two quotes immediately before the closing delimiter.
				line = strings.TrimLeft(line, quote[:1])
			}
			quote = ""
			continue
		}
		line = line[1:]
	}
	return quote
}

func appendPluginBlock(base, contents []byte) []byte {
	if len(contents) == 0 {
		return base
	}
	if len(base) != 0 && base[len(base)-1] != '\n' {
		base = append(base, '\n')
	}
	base = append(base, pluginBlockStart+"\n"...)
	base = append(base, contents...)
	if base[len(base)-1] != '\n' {
		base = append(base, '\n')
	}
	return append(base, pluginBlockEnd+"\n"...)
}

// WriteCodexPlugins replaces the app-owned block and preserves native project
// settings written by the user or workspace script.
func WriteCodexPlugins(workspace string, generated []byte) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := ensureDir(root, ".codex"); err != nil {
		return err
	}
	lock, err := lockConfig(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	prior, err := readRegular(root, codexConfig)
	if err != nil {
		return err
	}
	// v0.22.4 accepts only its original ownership header and regenerates the
	// entire file. Restore native settings only after observing that old shape.
	working := prior
	doc, err := decodeTOML(working)
	if err != nil {
		return err
	}
	legacy := bytes.HasPrefix(working, []byte(legacyPluginMarker))
	if legacy {
		_, owned, err := splitPluginBlock(working)
		if err != nil {
			return err
		}
		if _, shapeErr := pluginServers(doc); owned == nil && shapeErr == nil {
			working, err = readRegular(root, codexRollbackCopy)
			if err != nil {
				return err
			}
			doc, err = decodeTOML(working)
			if err != nil {
				return err
			}
		}
	}
	base, owned, err := splitPluginBlock(working)
	if err != nil {
		return err
	}
	baseDoc := doc
	if owned != nil {
		baseDoc, err = decodeTOML(base)
		if err != nil {
			return err
		}
		if err := verifyPluginProjection(doc, baseDoc, owned); err != nil {
			return err
		}
	}
	// Append on a fresh backing slice because base may still alias prior.
	base = bytes.Clone(base)
	compat := bytes.HasPrefix(generated, []byte(legacyPluginMarker))
	if compat {
		if !bytes.HasPrefix(base, []byte(legacyPluginMarker)) {
			base = append([]byte(legacyPluginMarker), base...)
		}
		// Keep the block even for an empty selection to distinguish new files
		// from a projection written by the old binary.
		generated = bytes.TrimPrefix(generated, []byte(legacyPluginMarker))
		if len(generated) == 0 {
			generated = []byte("\n")
		}
	}
	next := appendPluginBlock(base, generated)
	projected, err := decodeTOML(next)
	if err != nil {
		return err
	}
	if err := verifyPluginProjection(projected, baseDoc, generated); err != nil {
		return err
	}
	if compat {
		copy, err := readRegular(root, codexRollbackCopy)
		if err != nil {
			return err
		}
		if !bytes.Equal(copy, next) {
			if err := replace(root, codexRollbackCopy, copy, next); err != nil {
				return err
			}
		}
	}
	return writeCodex(root, prior, next)
}

func pluginServers(doc map[string]any) (map[string]any, error) {
	servers, err := table(doc)
	if err != nil {
		return nil, err
	}
	if len(doc) != 0 && (len(doc) != 1 || doc["mcp_servers"] == nil) {
		return nil, errors.New("unowned setting in managed Codex configuration")
	}
	for id, value := range servers {
		if _, ok := value.(map[string]any); !ok || !pluginServerID.MatchString(id) {
			return nil, errors.New("unowned setting in managed Codex configuration")
		}
	}
	return servers, nil
}

func verifyPluginProjection(full, base map[string]any, block []byte) error {
	owned, err := decodeTOML(block)
	if err != nil {
		return err
	}
	ownedServers, err := pluginServers(owned)
	if err != nil {
		return err
	}
	if len(owned) != 0 {
		servers, err := table(full)
		if err != nil {
			return err
		}
		baseServers, err := table(base)
		if err != nil {
			return err
		}
		for id, value := range ownedServers {
			if _, collision := baseServers[id]; collision || !sameTOML(servers[id], value) {
				return errors.New("managed Codex configuration conflicts with native settings")
			}
			delete(servers, id)
		}
		if len(servers) == 0 && base["mcp_servers"] == nil {
			delete(full, "mcp_servers")
		}
	}
	if !sameTOML(full, base) {
		return errors.New("managed Codex configuration would change native settings")
	}
	return nil
}

// Compare decoded TOML including NaNs, which reflect.DeepEqual treats as unequal.
func sameTOML(left, right any) bool {
	switch left := left.(type) {
	case map[string]any:
		right, ok := right.(map[string]any)
		return ok && maps.EqualFunc(left, right, sameTOML)
	case []any:
		right, ok := right.([]any)
		return ok && slices.EqualFunc(left, right, sameTOML)
	case []map[string]any:
		right, ok := right.([]map[string]any)
		return ok && slices.EqualFunc(left, right, func(a, b map[string]any) bool {
			return maps.EqualFunc(a, b, sameTOML)
		})
	case float64:
		right, ok := right.(float64)
		return ok && math.Float64bits(left) == math.Float64bits(right)
	default:
		return reflect.DeepEqual(left, right)
	}
}

func writeCodex(root *os.Root, prior, next []byte) error {
	if bytes.Equal(prior, next) && prior != nil {
		return nil
	}
	return replace(root, codexConfig, prior, next)
}

// CodexRevision detects changes to the Bot's native project file without
// observing or querying any unrelated project or user configuration.
func CodexRevision(workspace string) ([32]byte, error) {
	if workspace == "" {
		return [32]byte{}, nil
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return [32]byte{}, err
	}
	defer root.Close()
	body, err := readRegular(root, filepath.FromSlash(codexConfig))
	if err != nil {
		return [32]byte{}, err
	}
	if _, err := decodeTOML(body); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}
