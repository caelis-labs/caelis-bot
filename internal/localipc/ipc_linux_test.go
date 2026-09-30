package localipc

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateEndpointRoundTripPermissionsAndCleanup(t *testing.T) {
	listener, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for path, mode := range map[string]os.FileMode{listener.Endpoint(): 0600, filepath.Dir(listener.Endpoint()): 0700} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != mode {
			t.Fatal("endpoint permissions", err)
		}
	}
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_, e = io.CopyN(c, c, 4)
		done <- e
	}()
	conn, err := Dial(listener.Endpoint(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "ping" {
		t.Fatal("roundtrip failed", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err = listener.Close(); err != nil {
		t.Fatal("non-idempotent close", err)
	}
	if _, err = os.Stat(filepath.Dir(listener.Endpoint())); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("private directory leaked")
	}
}

func TestLinuxIPCRejectsNonPrivateEndpoints(t *testing.T) {
	listener, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := Dial("relative.sock", time.Second); err == nil {
		t.Fatal("accepted relative endpoint")
	}
	if err := os.Chmod(listener.Endpoint(), 0666); err != nil {
		t.Fatal(err)
	}
	if c, err := Dial(listener.Endpoint(), time.Second); err == nil {
		c.Close()
		t.Fatal("accepted public socket")
	}
	if err := os.Chmod(listener.Endpoint(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(listener.Endpoint()), 0755); err != nil {
		t.Fatal(err)
	}
	if c, err := Dial(listener.Endpoint(), time.Second); err == nil {
		c.Close()
		t.Fatal("accepted public directory")
	}
	if err := os.Chmod(filepath.Dir(listener.Endpoint()), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(listener.Endpoint()), "link.sock")
	if err := os.Symlink(listener.Endpoint(), link); err != nil {
		t.Fatal(err)
	}
	if c, err := Dial(link, time.Second); err == nil {
		c.Close()
		t.Fatal("accepted symlink")
	}
}

func TestLinuxIPCCloseReleasesAcceptAndAllowsAcceptedConnectionDrain(t *testing.T) {
	listener, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := Dial(listener.Endpoint(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	blocked := make(chan error, 1)
	go func() { _, err := listener.Accept(); blocked <- err }()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-blocked:
		if err == nil {
			t.Fatal("Accept returned no close error")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not release Accept")
	}
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_ = server.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1)
	if _, err := io.ReadFull(server, data); err != nil || data[0] != 'x' {
		t.Fatalf("accepted connection could not drain: %v", err)
	}
	if c, err := Dial(listener.Endpoint(), time.Second); err == nil {
		c.Close()
		t.Fatal("closed endpoint still accepts clients")
	}
}
