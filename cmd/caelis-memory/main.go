// caelis-memory is an explicitly offline file operation. It does not connect to
// a Runtime or transfer files over SSH; copy the private bundle separately.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
)

type attachments []string

func (a *attachments) String() string         { return strings.Join(*a, ",") }
func (a *attachments) Set(value string) error { *a = append(*a, value); return nil }

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: caelis-memory export|import --source-stopped (use --help for the selected operation)")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	stopped := flags.Bool("source-stopped", false, "confirm source Bot, direct writers and automatic restart remain stopped")
	bundle := flags.String("bundle", "", "absolute private bundle directory")
	var out memorytransfer.Result
	var err error
	switch args[0] {
	case "export":
		source := flags.String("source", "", "absolute stopped source profile directory")
		var files attachments
		flags.Var(&files, "attachment", "referenced nonsecret Notebook-relative attachment (repeatable)")
		if err = flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected positional arguments")
		}
		out, err = memorytransfer.Export(ctx, memorytransfer.ExportOptions{Source: *source, Bundle: *bundle, SourceStopped: *stopped, Attachments: files})
	case "import":
		destination := flags.String("destination", "", "absolute absent target profile directory; existing profiles are never replaced")
		targetStopped := flags.Bool("destination-stopped", false, "confirm target Bot and direct writers remain stopped")
		if err = flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected positional arguments")
		}
		out, err = memorytransfer.Import(ctx, memorytransfer.ImportOptions{Bundle: *bundle, Destination: *destination, SourceStopped: *stopped, DestinationStopped: *targetStopped})
	default:
		return fmt.Errorf("unknown operation %q", args[0])
	}
	if out.Path != "" {
		if e := json.NewEncoder(os.Stdout).Encode(out); e != nil {
			return e
		}
	}
	return err
}
