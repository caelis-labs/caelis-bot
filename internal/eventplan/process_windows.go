package eventplan

import "os/exec"

// Script collectors are not advertised on Windows until owned process-tree
// cancellation is wired; this fallback only serves compilation.
func boundCollector(cmd *exec.Cmd) func() {
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}
