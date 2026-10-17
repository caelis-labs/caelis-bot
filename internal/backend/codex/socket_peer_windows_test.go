package codex

import (
	"golang.org/x/sys/windows"
	"net"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestWindowsSocketPeerIdentity(t *testing.T) {
	dir, err := os.MkdirTemp("", "cb-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "peer.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	accepted, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	raw, _ := c.(*net.UnixConn).SyscallConn()
	var pid uint64
	var returned uint32
	_ = raw.Control(func(fd uintptr) {
		err = windows.WSAIoctl(windows.Handle(fd), 0x58000100, nil, 0, (*byte)(unsafe.Pointer(&pid)), 8, &returned, nil, 0)
	})
	if err != nil || pid != uint64(os.Getpid()) {
		t.Fatalf("peer pid=%d returned=%d error=%v", pid, returned, err)
	}
	if got := readSocketPeerIdentity(c); got.PID != os.Getpid() {
		t.Fatalf("peer projection=%+v native bytes=%d", got, returned)
	}
}
