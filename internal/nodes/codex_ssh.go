package nodes

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// CodexSSHConfig is selected native configuration; Socket is the target node
// service's private wire socket, not Codex's endpoint or any credential file.
type CodexSSHConfig struct {
	Destination, Helper, Socket, Binary string
	Pair                                workerwire.Pair
	Source                              api.WorkSourceProvider
}

type CodexSSHWorker struct{ *workerwire.Client }

func NewCodexSSHWorker(ctx context.Context, config CodexSSHConfig) (*CodexSSHWorker, error) {
	if !sshTarget.MatchString(config.Destination) || !path.IsAbs(config.Socket) || strings.ContainsAny(config.Socket, "\x00\r\n") || strings.ContainsAny(config.Helper, "\x00\r\n") {
		return nil, errors.New("invalid native Codex Worker target")
	}
	if config.Helper == "" {
		config.Helper = "caelis-node"
	}
	if config.Binary == "" {
		config.Binary = "ssh"
	}
	base := &SSHWorker{config: SSHConfig{Binary: config.Binary, Target: config.Destination}}
	command := "exec " + sshQuote(config.Helper) + " proxy-worker --socket " + sshQuote(config.Socket)
	args := append(base.args(), "--", config.Destination, command)
	cmd := exec.Command(config.Binary, args...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("Worker SSH input unavailable")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, errors.New("Worker SSH output unavailable")
	}
	if err = ctx.Err(); err == nil {
		err = cmd.Start()
	}
	if err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, errors.New("Worker SSH connection unavailable")
	}
	stream := &sshWorkerStream{Reader: out, Writer: in, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(stream.done) }()
	client, err := workerwire.NewClient(ctx, config.Pair, config.Source, stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	return &CodexSSHWorker{Client: client}, nil
}

type sshWorkerStream struct {
	io.Reader
	io.Writer
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

func (s *sshWorkerStream) Close() error {
	s.once.Do(func() {
		if c, ok := s.Writer.(io.Closer); ok {
			_ = c.Close()
		}
		if c, ok := s.Reader.(io.Closer); ok {
			_ = c.Close()
		}
		_ = s.cmd.Process.Kill()
		<-s.done
	})
	return nil
}

var _ api.WorkRuntime = (*CodexSSHWorker)(nil)
