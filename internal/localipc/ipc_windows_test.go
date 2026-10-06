//go:build windows

package localipc

import "testing"

func TestWindowsIPCRequiresPrivateNamedPipeAdapter(t *testing.T) {
	if listener, err := Listen(); listener != nil || err == nil {
		t.Fatalf("unsupported IPC must fail before exposing a listener: %v", err)
	}
}
