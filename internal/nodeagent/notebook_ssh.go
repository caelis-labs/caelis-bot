package nodeagent

import "context"

// NotebookSSH validates native ssh -G metadata and keeps the enrolled alias in
// its original SSH configuration namespace, including ProxyJump hop aliases.
// A flattened -F profile would lose those aliases' own connection/trust settings.
// It never reads credential contents or creates another connection definition.
func NotebookSSH(ctx context.Context, s SSHConfig) ([]string, func(), error) {
	args, err := s.args()
	if err != nil {
		return nil, nil, err
	}
	effective, err := effectiveSSHConfiguration(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	if _, err = sanitizedSSHConfiguration(effective, true); err != nil {
		return nil, nil, err
	}
	args = append([]string{s.binary()}, args...)
	args = append(args, "-o", "ClearAllForwardings=yes")
	return args, func() {}, nil
}
