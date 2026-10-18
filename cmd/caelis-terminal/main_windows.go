package main

import (
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"log"
	"os"
)

// A console-subsystem helper is necessary: GUI-subsystem programs do not
// reliably inherit a host's real console when launched by conhost or ConPTY.
func main() {
	handled, err := taskterminal.RunClient(os.Args[1:])
	if !handled {
		log.Fatal("native terminal descriptor required")
	}
	if err != nil {
		log.Fatal(err)
	}
}
