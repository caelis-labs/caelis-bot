package nodeagent

import "context"

// NotebookSSH reuses the existing, sanitized SSH connection and host trust for
// ordinary rsync. It reads only ssh -G metadata, never credential file contents.
// The temporary metadata configuration must outlive the transfer and be closed.
func NotebookSSH(ctx context.Context, s SSHConfig) ([]string, func(), error) {
	args, err := s.args()
	if err != nil {
		return nil, nil, err
	}
	path, cleanup, err := resolvedJoinConfig(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	args = append([]string{s.binary(), "-F", path}, args...)
	args = append(args, "-o", "ClearAllForwardings=yes")
	return args, cleanup, nil
}
