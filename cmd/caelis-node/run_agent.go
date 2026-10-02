package main

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"io"
	"os"
)

func runAgent(ctx context.Context, args []string, out io.Writer) error {
	return nodeagent.Run(ctx, args, os.Stdin, out)
}
