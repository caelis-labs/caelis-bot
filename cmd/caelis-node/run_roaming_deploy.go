package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/app"
)

// Deployment is an explicit reviewed native operation. This command cannot
// choose credentials, configure SSH, install a service or accept shell text.
func runRoamingDeploy(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "supervise-roaming" && args[0] != "control-roaming" {
		return errors.New("native deployment supervisor command required")
	}
	if args[0] == "supervise-roaming" {
		signal.Ignore(syscall.SIGHUP)
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(out)
	plan := f.String("plan-file", "", "exact approved private native deployment plan")
	op := f.String("operation-id", "", "original explicit reviewed disable operation")
	state := f.String("state", "", "closed disabling or disabled deployment state")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	if f.NArg() != 0 || !filepath.IsAbs(*plan) {
		return errors.New("explicit absolute approved native plan required")
	}
	if args[0] == "control-roaming" {
		return app.SetNodeRoamingSupervisorState(*plan, *op, *state)
	}
	if *op != "" || *state != "" {
		return errors.New("supervisor start cannot include disable control")
	}
	return app.RunNodeRoamingSupervisor(ctx, *plan)
}
