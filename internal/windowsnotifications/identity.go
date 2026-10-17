package windowsnotifications

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Profiles have separate notification and activation identities. An activation
// is only a request to open chat; no notification text becomes an instruction.
func Identity(root string) (appID, scheme string) {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	suffix := hex.EncodeToString(sum[:8])
	return "Caelis.Bot." + suffix, "caelis-bot-" + suffix
}
func ParseActivation(args []string) (root string, handled bool, err error) {
	if len(args) == 0 || args[0] != "--notification-chat" {
		return "", false, nil
	}
	if len(args) != 3 || !filepath.IsAbs(args[1]) {
		return "", true, errors.New("invalid notification activation")
	}
	root = filepath.Clean(args[1])
	_, scheme := Identity(root)
	if args[2] != scheme+":chat" {
		return "", true, errors.New("invalid notification action")
	}
	info, e := os.Stat(root)
	if e != nil || !info.IsDir() {
		return "", true, errors.New("notification profile unavailable")
	}
	return root, true, nil
}
func Payload(scheme, title, body, logo string) (string, error) {
	if len(title) > 2048 || len(body) > 8192 || strings.ContainsRune(title+body, 0) {
		return "", errors.New("invalid notification content")
	}
	escape := func(s string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	return `<toast activationType="protocol" launch="` + escape(scheme+":chat") + `"><visual><binding template="ToastGeneric"><image placement="appLogoOverride" hint-crop="circle" src="` + escape(logo) + `"/><text>` + escape(title) + `</text><text>` + escape(body) + `</text></binding></visual></toast>`, nil
}
