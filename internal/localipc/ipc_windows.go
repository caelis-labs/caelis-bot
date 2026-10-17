package localipc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func currentSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

// go-winio creates the first instance exclusively and rejects remote clients.
// The protected DACL admits only this user. Payload tokens remain mandatory.
func Listen() (*Listener, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	address := `\\.\pipe\caelis-bot-` + hex.EncodeToString(nonce[:])
	native, err := winio.ListenPipe(address, &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)", sid),
		InputBufferSize:    65536, OutputBufferSize: 65536,
	})
	if err != nil {
		return nil, err
	}
	return &Listener{Listener: &peerListener{Listener: native, sid: sid},
		endpoint: address, transport: NamedPipe, cleanup: native.Close}, nil
}

type peerListener struct {
	net.Listener
	sid string
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if err = verifyPeer(conn, l.sid, false); err == nil {
			return conn, nil
		}
		_ = conn.Close()
	}
}

func Dial(endpoint string, timeout time.Duration) (net.Conn, error) {
	const prefix = `\\.\pipe\caelis-bot-`
	if !strings.HasPrefix(endpoint, prefix) || len(endpoint) != len(prefix)+48 {
		return nil, errors.New("invalid private Bot pipe")
	}
	if _, err := hex.DecodeString(endpoint[len(prefix):]); err != nil {
		return nil, err
	}
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	conn, err := winio.DialPipe(endpoint, &timeout)
	if err != nil {
		return nil, err
	}
	if err = verifyPeer(conn, sid, true); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func verifyPeer(conn net.Conn, sid string, server bool) error {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("pipe peer handle unavailable")
	}
	var pid uint32
	var err error
	if server {
		err = windows.GetNamedPipeServerProcessId(windows.Handle(fd.Fd()), &pid)
	} else {
		err = windows.GetNamedPipeClientProcessId(windows.Handle(fd.Fd()), &pid)
	}
	if err != nil || pid == 0 {
		return errors.New("pipe peer identity unavailable")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err = windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || user.User.Sid.String() != sid {
		return errors.New("pipe peer user mismatch")
	}
	// Recheck the pipe binding after opening the stable process handle.
	var confirmed uint32
	if server {
		err = windows.GetNamedPipeServerProcessId(windows.Handle(fd.Fd()), &confirmed)
	} else {
		err = windows.GetNamedPipeClientProcessId(windows.Handle(fd.Fd()), &confirmed)
	}
	if err != nil || confirmed != pid {
		return errors.New("pipe peer changed")
	}
	return nil
}
