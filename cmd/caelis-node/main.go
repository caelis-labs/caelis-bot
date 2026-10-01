// caelis-node is an explicitly launched native owner. It does not install a
// daemon, discover user profiles or transfer Runtime credentials.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/bot"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--bot-tools" {
		if err := bot.RunStdio(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if len(os.Args) > 1 && os.Args[1] == "supervise-roaming" {
		// An explicitly approved independent node must survive its initiating
		// SSH client. Preserve nohup semantics instead of registering SIGHUP.
		signal.Ignore(syscall.SIGHUP)
	} else {
		signals = append(signals, syscall.SIGHUP)
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
