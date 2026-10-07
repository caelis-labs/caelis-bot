package codex

import "strconv"

func appServerArgs(opts Options, listen string) []string {
	args := []string{"app-server", "--listen", listen}
	if opts.TrustedProject {
		args = append(args, "-c", "projects={"+strconv.Quote(opts.Directory)+"={ trust_level=\"trusted\" }}")
	}
	return args
}
