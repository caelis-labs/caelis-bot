package app

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func productSSHArgs(pairing backend.ProductPairing) ([]string, error) {
	if err := validateProductPairing(pairing); err != nil || pairing.Mode != "remote" {
		return nil, errors.New("invalid native product SSH pairing")
	}
	helper := pairing.Helper
	if helper == "" {
		helper = "caelis-node"
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	command := quote(helper) + " proxy-product --endpoint " + quote(pairing.Endpoint) + " --auth-file " + quote(pairing.AuthFile)
	return []string{"-T", "-o", "BatchMode=yes", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ForwardX11Trusted=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ForkAfterAuthentication=no", "-o", "UpdateHostKeys=no", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10", "-o", "ControlMaster=no", "-o", "ControlPath=none", "--", pairing.SSH, command}, nil
}

func newSSHProductClient(pairing backend.ProductPairing) (nativeProductClient, io.Closer, error) {
	args, err := productSSHArgs(pairing)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command("ssh", args...)
	// All target diagnostics remain private to the target. APP reports only a
	// local categorical connection failure, never SSH/config/auth stderr.
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, errors.New("native product input pipe unavailable")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, nil, errors.New("native product output pipe unavailable")
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, nil, errors.New("existing SSH client could not start")
	}
	stream := &productSSHStream{input: in, output: out, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(stream.done) }()
	client, err := productrpc.NewStdioClient(productrpc.StdioOptions{ExpectedNode: pairing.NodeID, ExpectedBot: pairing.BotID}, stream)
	if err != nil {
		stream.Close()
		return nil, nil, errors.New("native product transport is incompatible")
	}
	return client, stream, nil
}

type productSSHStream struct {
	input  io.WriteCloser
	output io.ReadCloser
	cmd    *exec.Cmd
	done   chan struct{}
	once   sync.Once
}

func (s *productSSHStream) Read(b []byte) (int, error)  { return s.output.Read(b) }
func (s *productSSHStream) Write(b []byte) (int, error) { return s.input.Write(b) }
func (s *productSSHStream) Close() error {
	s.once.Do(func() {
		_ = s.input.Close()
		_ = s.output.Close()
		select {
		case <-s.done:
			return
		case <-time.After(100 * time.Millisecond):
		}
		// This exact process was created above. No app/process-group/PID lookup
		// can select the existing desktop Bot or the target resident service.
		_ = s.cmd.Process.Kill()
	})
	return nil
}
