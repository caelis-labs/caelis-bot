//go:build windows

package localipc

import (
	"io"
	"testing"
	"time"
)

func TestWindowsPrivateNamedPipeRoundTrip(t *testing.T) {
	listener, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Address().Transport != NamedPipe {
		t.Fatal("wrong transport")
	}
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		_, e = io.CopyN(c, c, 4)
		done <- e
	}()
	c, err := Dial(listener.Endpoint(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	_, err = c.Write([]byte("ping"))
	if err != nil {
		t.Fatal(err)
	}
	var got [4]byte
	if _, err = io.ReadFull(c, got[:]); err != nil || string(got[:]) != "ping" {
		t.Fatalf("round trip failed: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = Dial(`\\remote\pipe\caelis-bot-test`, time.Millisecond); err == nil {
		t.Fatal("remote endpoint accepted")
	}
}
