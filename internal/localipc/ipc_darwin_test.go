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
