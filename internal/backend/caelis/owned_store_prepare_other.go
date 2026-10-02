//go:build !darwin && !linux

package caelis

import "errors"

func PrepareOwnedStore(string, string) error {
	return errors.New("native owned Caelis Store preparation unsupported")
}
