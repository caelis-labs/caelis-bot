package botmemory

import (
	"os"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.RunStateIOTrace(m, "botmemory")) }
