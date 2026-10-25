package sshtransport

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureTransport(t *testing.T) *Transport {
	t.Helper()
	root := t.TempDir()
	v := New(filepath.Join(root, "Bot Data", "SSH"))
	v.home = filepath.Join(root, "home")
	v.system = filepath.Join(root, "system", "ssh_config")
	for _, p := range []string{v.root, filepath.Join(v.home, ".ssh"), filepath.Dir(v.system)} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(v.Close)
	return v
}
func put(t *testing.T, path, text string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestConfigurationIsInertAndSaveChecksRevision(t *testing.T) {
	v := fixtureTransport(t)
	marker := filepath.Join(v.root, "must-not-execute")
	put(t, v.path(), "Host private\n HostName 127.0.0.1\n")
	put(t, filepath.Join(v.home, ".ssh", "config"), "Include included\nMatch exec \"touch "+marker+"\"\n User unsafe\n")
	put(t, filepath.Join(v.home, ".ssh", "included"), "Host user user-two *.not-literal !excluded\n ProxyJump jump\n")
	put(t, v.system, "Include conf.d/*.conf\n")
	put(t, filepath.Join(filepath.Dir(v.system), "conf.d", "10.conf"), "Host system\n")
	a, e := v.Configuration()
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(a.Hosts, ",") != "private,system,user,user-two" {
		t.Fatal(a.Hosts)
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("preview executed Match exec")
	}
	saved, e := v.Save(t.Context(), api.SSHConfigurationSave{Revision: a.Revision, Text: "Host updated\n HostName 127.0.0.1\n"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("validation executed Match exec")
	}
	info, _ := os.Stat(v.path())
	if !localstate.HasPrivateMode(v.path(), info, 0600) {
		t.Fatal(info.Mode())
	}
	put(t, v.path(), "# external edit\n")
	if _, e = v.Save(t.Context(), api.SSHConfigurationSave{Revision: saved.Revision, Text: "# stale"}); e == nil || e.Error() != "ssh_config_changed" {
		t.Fatal(e)
	}
	bad, _ := v.Configuration()
	if _, e = v.Save(t.Context(), api.SSHConfigurationSave{Revision: bad.Revision, Text: "NotAnSSHOption yes\n"}); e == nil || e.Error() != "ssh_config_syntax" {
		t.Fatal(e)
	}
}
func TestSystemSSHResolvesPriorityAndRelativeSystemIncludes(t *testing.T) {
	v := fixtureTransport(t)
	put(t, v.path(), "Host target\n User bot-user\nHost *\n")
	put(t, filepath.Join(v.home, ".ssh", "config"), "Host target\n User ordinary-user\n HostName 127.0.0.2\n ProxyJump jump\nHost jump\n HostName 127.0.0.3\n")
	put(t, v.system, "Include conf.d/*.conf\n")
	put(t, filepath.Join(filepath.Dir(v.system), "conf.d", "1.conf"), "Include nested.conf\nHost target\n Port 2222\n")
	put(t, filepath.Join(filepath.Dir(v.system), "nested.conf"), "Host target\n ServerAliveInterval 37\n")
	dir := t.TempDir()
	p, e := v.compose(dir, false, "")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), v.binary, "-G", "-F", p, "target")
	b, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"user bot-user\n", "hostname 127.0.0.2\n", "port 2222\n", "proxyjump jump\n", "serveraliveinterval 37\n", "clearallforwardings yes\n", "controlmaster false\n"} {
		if !strings.Contains(strings.ReplaceAll(string(b), "\r\n", "\n"), s) {
			t.Fatalf("missing %q in ssh -G", s)
		}
	}
}
func TestTargetRejectsShellOptionsAndKeepsBotPortSeparate(t *testing.T) {
	for _, bad := range []string{"-Fconfig", "x;touch", "user@host", "host/name", "x$(id)", "foo bar"} {
		if _, e := Address(api.SSHTarget{Host: bad}); e == nil {
			t.Fatal(bad)
		}
	}
	a, e := Address(api.SSHTarget{Host: "fedora", Port: 29317})
	if e != nil || a != "ssh://fedora:29317" {
		t.Fatal(a, e)
	}
	v, ok := Target(a)
	if !ok || v.Port != 29317 {
		t.Fatal(v)
	}
	x := New(t.TempDir())
	defer x.Close()
	if a, e = x.Resolve(context.Background(), "http://127.0.0.1:9317"); e != nil || a != "http://127.0.0.1:9317" {
		t.Fatal(a, e)
	}
}
func TestExternalEditDuringValidationWins(t *testing.T) {
	v := fixtureTransport(t)
	a, _ := v.Configuration()
	script := filepath.Join(t.TempDir(), "fake-ssh")
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fixture script")
	}
	put(t, script, "#!/bin/sh\nprintf '# external\\n' > "+"'"+v.path()+"'"+"\n")
	os.Chmod(script, 0700)
	v.binary = script
	if _, e := v.Save(t.Context(), api.SSHConfigurationSave{Revision: a.Revision, Text: "# draft\n"}); e == nil || e.Error() != "ssh_config_changed" {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(v.path())
	if string(b) != "# external\n" {
		t.Fatal("external edit overwritten")
	}
}
func TestDynamicSystemIncludeFailsExplicitlyAtConnection(t *testing.T) {
	v := fixtureTransport(t)
	put(t, v.system, "Include ${HOME}/conditional\n")
	if _, e := v.compose(t.TempDir(), false, ""); e == nil || e.Error() != "ssh_system_include_dynamic" {
		t.Fatal(e)
	}
}
func TestInertIndexAndValidationNeverMirrorPrivateKeyFile(t *testing.T) {
	v := fixtureTransport(t)
	fake := filepath.Join(v.home, ".ssh", "fake_key")
	put(t, fake, "-----BEGIN OPENSSH PRIVATE KEY-----\nSENTINEL-FAKE-NOT-A-KEY\n-----END OPENSSH PRIVATE KEY-----\n")
	put(t, filepath.Join(v.home, ".ssh", "config"), "Include "+quote(fake)+"\n")
	a, e := v.Configuration()
	if e != nil {
		t.Fatal(e)
	}
	if a.Issue != "ssh_config_partial_index" {
		t.Fatal("key Include was not marked incomplete")
	}
	dir := t.TempDir()
	if _, e = v.compose(dir, true, "# draft\n"); e == nil {
		t.Fatal("key Include accepted")
	}
	files, _ := os.ReadDir(dir)
	for _, p := range files {
		b, _ := os.ReadFile(filepath.Join(dir, p.Name()))
		if strings.Contains(string(b), "SENTINEL-FAKE") {
			t.Fatal("private key mirrored")
		}
	}
}
