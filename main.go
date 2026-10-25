package main

import (
	"embed"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/headless"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"github.com/caelis-labs/caelis-bot/internal/windowsupdate"
	"io/fs"
	"log"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "serve" || os.Args[1] == "pair") {
		webAssets, err := fs.Sub(assets, "frontend/dist")
		if err == nil {
			err = headless.Run(webAssets, os.Args[1:], updates.Version)
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if handled, err := desktop.HandleNativeArguments(os.Args[1:]); handled {
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if handled, err := windowsupdate.Run(os.Args[1:]); handled {
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--terminal-smoke" {
		if err := desktop.RunTerminalSmoke(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--bot-tools" {
		if err := bot.RunStdio(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--plugin-mcp" {
		if len(os.Args) != 5 && len(os.Args) != 6 && len(os.Args) != 7 {
			os.Exit(2)
		}
		if err := plugins.RunStdio(os.Args[2], os.Args[3], os.Args[4], os.Stdin, os.Stdout, os.Stderr, os.Args[5:]...); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := desktop.Run(assets); err != nil {
		log.Fatal(err)
	}
}
