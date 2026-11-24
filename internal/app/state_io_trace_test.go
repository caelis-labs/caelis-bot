package app

import (
	"os"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/testutil"
)

func TestMain(m *testing.M) {
	if os.Getenv("BOT_UPGRADE_CLI_FIXTURE") == "1" {
		os.Exit(runUpgradeCLIFixture())
	}
	os.Exit(testutil.RunStateIOTrace(m, "app"))
}
