package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
	"os"
	"time"
)

func main() {
	directory := flag.String("directory", "", "Explicit private installation directory beneath the target user's home")
	flag.Parse()
	if *directory == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "An explicit private installation directory is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := runtimemanagement.RunCommand(ctx, *directory, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Runtime management request did not complete successfully")
		os.Exit(1)
	}
}
