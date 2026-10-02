//go:build !darwin

package desktop

func canonicalExistingProfilePath(path string) (string, error) { return path, nil }
