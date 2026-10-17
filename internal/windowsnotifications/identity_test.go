package windowsnotifications

import (
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationActivationOnlyOpensExactProfileChat(t *testing.T) {
	root := t.TempDir()
	_, scheme := Identity(root)
	got, handled, err := ParseActivation([]string{"--notification-chat", root, scheme + ":chat"})
	if err != nil || !handled || got != filepath.Clean(root) {
		t.Fatal(got, handled, err)
	}
	for _, args := range [][]string{{"--notification-chat", root, scheme + ":chat?prompt=run"}, {"--notification-chat", root, scheme + ":chat", "--capture"}, {"--notification-chat", "relative", scheme + ":chat"}} {
		if _, handled, err := ParseActivation(args); !handled || err == nil {
			t.Fatal("untrusted notification action accepted")
		}
	}
	other, _ := Identity(filepath.Join(root, "other"))
	id, _ := Identity(root)
	if other == id {
		t.Fatal("profile identities overlap")
	}
}
func TestToastContentIsEscapedAndCannotBecomeActivation(t *testing.T) {
	payload, err := Payload("caelis-test", `<title & "hi">`, `</text><actions><action arguments="execute"/></actions>`, `file:///C:/icon.png`)
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(payload))
	actions := 0
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "actions" {
			actions++
		}
	}
	if actions != 0 || !strings.Contains(payload, `launch="caelis-test:chat"`) {
		t.Fatal("notification content became authority")
	}
}
