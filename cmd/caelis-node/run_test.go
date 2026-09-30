package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductTokenIsExplicitPrivateAndUnrelatedToRuntime(t *testing.T) {
	file := filepath.Join(t.TempDir(), "product.auth")
	token := strings.Repeat("x", 64)
	if err := os.WriteFile(file, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readProductToken(file); err != nil || got != token {
		t.Fatal("private token unreadable", err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readProductToken(file); err == nil {
		t.Fatal("public auth file accepted")
	}
	link := filepath.Join(t.TempDir(), "auth-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProductToken(link); err == nil {
		t.Fatal("linked auth file accepted")
	}
}

func TestProfileOwnerIsExclusiveAndIdentityStable(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "owner.lock")
	unlock, err := lockProfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lockProfile(lock); err == nil {
		t.Fatal("second resident owner acquired profile")
	}
	unlock()
	unlock, err = lockProfile(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = os.WriteFile(filepath.Join(root, "bot.json"), []byte(`{"id":"synthetic-native-resident"}`), 0600); err != nil {
		t.Fatal(err)
	}
	node, bot, err := profileIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	node2, bot2, err := profileIdentity(root)
	if err != nil || node != node2 || bot != bot2 || strings.Contains(bot, "synthetic-native") {
		t.Fatal("product identity changed/exposed native binding", err)
	}
}

func TestHeadlessRequiresExplicitProfileAndLoopback(t *testing.T) {
	for _, args := range [][]string{{}, {"serve-bot"}, {"serve-bot", "--profile", "relative", "--auth-file", "/not-read"}, {"serve-bot", "--profile", "/not-created", "--auth-file", "/not-read", "--listen", "0.0.0.0:9"}, {"serve-bot", "--profile", "/not-created", "--auth-file", "/not-read", "--listen", "localhost:9"}} {
		if err := run(t.Context(), args, new(bytes.Buffer)); err == nil {
			t.Fatal("unsafe/incomplete headless configuration accepted")
		}
	}
}
