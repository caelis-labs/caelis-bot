package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
	"os"
	"strings"
	"time"
)

func main() {
	directory := flag.String("directory", "", "Explicit private installation directory beneath the target user's home")
	importArchive := flag.String("import-archive", "", "Native-only reviewed runtime@version archive import from stdin")
	flag.Parse()
	if *directory == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "An explicit private installation directory is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if *importArchive != "" {
		provider, version, ok := strings.Cut(*importArchive, "@")
		manager, err := runtimemanagement.New(*directory)
		if !ok || err != nil {
			fmt.Fprintln(os.Stderr, "Archive import requires a private directory and reviewed runtime@version")
			os.Exit(2)
		}
		if err := manager.ImportArchive(ctx, provider, version, os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "Reviewed archive import did not complete successfully")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "Reviewed archive verified and cached; runtime selection unchanged")
		return
	}
	if err := runtimemanagement.RunCommand(ctx, *directory, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Runtime management request did not complete successfully")
		os.Exit(1)
	}
}
