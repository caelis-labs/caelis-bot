package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"

	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// This calls the existing complete Runtime's serve-worker owner and Worker wire
// through enrolled SSH. No agent proxy, renderer path or credential is involved.
func OpenEnrolledWorkerStream(owner, ctx context.Context, ssh SSHConfig, helper, directory, node string, pair workerwire.Pair) (io.ReadWriteCloser, error) {
	args, err := ssh.args()
	if err != nil || owner == nil || ctx.Err() != nil || !cleanOwnedRuntimePath(helper) || !cleanOwnedRuntimePath(directory) || pair.Target.NodeID != node || pair.Target.Backend != "codex" || workerwire.ValidateRelayPair(pair) != nil {
		return nil, errors.New("exact enrolled Worker route required")
	}
	args = append(args, "-o", "ClearAllForwardings=yes", "--", ssh.Target, shellQuote(helper)+" connect-enrolled-worker --directory "+shellQuote(directory)+" --node-id "+shellQuote(node))
	cmd := exec.CommandContext(owner, ssh.binary(), args...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, errors.New("enrolled Worker SSH unavailable")
	}
	stream := &sshStream{stdin: in, stdout: out, cmd: cmd}
	if err = json.NewEncoder(in).Encode(pair); err != nil {
		stream.Close()
		return nil, err
	}
	return stream, nil
}
