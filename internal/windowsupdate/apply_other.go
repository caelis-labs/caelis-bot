//go:build !windows

package windowsupdate

func Run([]string) (bool, error) { return false, nil }
