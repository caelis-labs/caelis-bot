// caelis-agent is the lightweight explicitly launched node proxy/installer.
// It does not import APP/Wails/GUI composition or install a persistent service.
package main

import (
	"context"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := nodeagent.Run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
