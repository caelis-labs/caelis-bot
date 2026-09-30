// caelis-worker-bootstrap is a target-side stdin/stdout helper, not a daemon.
package main

import (
	"context"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := caelis.RunWorkerBootstrap(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Worker bootstrap unavailable")
		os.Exit(1)
	}
}
