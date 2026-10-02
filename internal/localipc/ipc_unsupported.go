//go:build !darwin

package localipc

import (
	"errors"
	"net"
	"time"
)

var errUnsupported = errors.New("private Bot IPC is not implemented for this platform")

func Listen() (*Listener, error)                   { return nil, errUnsupported }
func Dial(string, time.Duration) (net.Conn, error) { return nil, errUnsupported }
