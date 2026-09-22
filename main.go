package main

import (
	"embed"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"log"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--bot-tools" {
		if err := bot.RunStdio(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := desktop.Run(assets); err != nil {
		log.Fatal(err)
	}
}
