// caelisbot-launcher is the stable, same-user owner of a managed installation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/managedupdate"
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("use init, upgrade or serve")
	}
	command := os.Args[1]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	root := f.String("root", "", "absolute private managed installation")
	profile := f.String("data-dir", "", "absolute private Bot profile (init only)")
	archive := f.String("archive", "", "signed archive (init/upgrade)")
	manifest := f.String("manifest", "", "signed manifest (init/upgrade)")
	signature := f.String("signature", "", "detached signature (init/upgrade)")
	qa := f.String("qa-trust", "", "isolated QA build trust file; rejected by production")
	listen := f.String("listen", "127.0.0.1:9317", "Bot listener (init only)")
	origin := f.String("origin", "", "exact browser origin (init only)")
	pair := f.Bool("require-pairing", false, "require browser pairing (init only)")
	wait := f.Bool("wait", false, "only wait for the scheduled offer's health (upgrade only)")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if *wait && command != "upgrade" {
		return fmt.Errorf("--wait is only valid with upgrade")
	}
	if f.NArg() != 0 || *root == "" || runtime.GOOS != "linux" {
		return fmt.Errorf("Linux and a managed root are required")
	}
	if managedupdate.Containerized() {
		return &managedupdate.Error{Code: "image_managed"}
	}
	trust := managedupdate.ProductionTrust()
	if *qa != "" {
		var e error
		trust, e = managedupdate.ReadQATrust(*qa)
		if e != nil {
			return e
		}
	}
	platform := "linux-" + runtime.GOARCH
	switch command {
	case "init", "upgrade":
		data, e := os.ReadFile(*manifest)
		if e != nil {
			return e
		}
		sig, e := os.ReadFile(*signature)
		if e != nil {
			return e
		}
		o, e := managedupdate.Verify(data, sig, trust.Key, platform, trust.QA)
		if e != nil {
			return e
		}
		if command == "upgrade" {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			state := managedupdate.UpgradeHealthy
			if *wait {
				e = managedupdate.AwaitLocalUpgrade(ctx, *root, o)
			} else {
				state, e = managedupdate.ScheduleLocalUpgrade(ctx, *root, *archive, o, trust, platform)
			}
			if e != nil {
				return e
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]managedupdate.LocalUpgradeResult{"state": state})
		}
		args := []string{"--listen", *listen}
		if *origin != "" {
			args = append(args, "--origin", *origin)
		}
		if *pair {
			args = append(args, "--require-pairing")
		}
		return managedupdate.Initialize(*root, *profile, *archive, o, trust, platform, args)
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return managedupdate.Launch(ctx, managedupdate.LaunchOptions{Root: *root, Platform: platform, Trust: trust})
	default:
		return fmt.Errorf("use init, upgrade or serve")
	}
}
func main() {
	if e := run(); e != nil {
		if managedupdate.Code(e) == "update_scheduled_waiting_idle" {
			fmt.Fprintln(os.Stderr, "Update scheduled, waiting for the existing Bot's idle handoff. The service is still running; the launcher was not replaced. Check Settings > Updates, then rerun --upgrade after installation completes.")
			os.Exit(1)
		}
		// Stable codes only. No private paths, arguments or conversation logs.
		fmt.Fprintln(os.Stderr, strings.TrimSpace(managedupdate.Code(e)))
		os.Exit(1)
	}
}
