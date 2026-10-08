package main

import (
	"embed"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
	"log"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--remote-terminal-smoke" {
		if len(os.Args) != 5 {
			log.Fatal("specify disposable data directory, owned task and terminal")
		}
		if err := desktop.RunRemoteTerminalSmoke(os.Args[2], os.Args[3], os.Args[4]); err != nil {
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
	if len(os.Args) > 1 && os.Args[1] == "--desktop-capture-smoke" {
		if len(os.Args) != 6 {
			log.Fatal("specify exact disposable window title, private evidence/image paths and once marker")
		}
		if err := desktop.RunDesktopCaptureSmoke(os.Args[2], os.Args[3], os.Args[4], os.Args[5]); err != nil {
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
		if len(os.Args) != 5 && len(os.Args) != 6 {
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
