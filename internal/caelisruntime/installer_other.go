//go:build !darwin

package caelisruntime

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

func runInstaller(context.Context, i18n.Locale, string, string) error {
	return errors.New("native installer is unavailable on this platform")
}
