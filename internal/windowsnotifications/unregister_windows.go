//go:build windows && amd64

package windowsnotifications

import (
	"errors"
	"golang.org/x/sys/windows/registry"
	"strings"
)

// Remove only identities still owned by this executable. Another portable
// installation's registration must survive uninstall of an older copy.
func Unregister(root, executable string) error {
	id, scheme := Identity(root)
	paths := []string{`Software\Classes\AppUserModelId\` + id, `Software\Classes\` + scheme}
	for _, path := range paths {
		key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		owner, _, err := key.GetStringValue("CaelisOwner")
		key.Close()
		if err != nil || !strings.EqualFold(owner, executable) {
			return errors.New("notification registration belongs to another installation")
		}
	}
	for _, path := range []string{paths[1] + `\shell\open\command`, paths[1] + `\shell\open`, paths[1] + `\shell`, paths[1], paths[0]} {
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
	}
	return nil
}
