package caelis

import (
	"errors"
	"os"
	"path/filepath"
)

func notebookSkillDirs(workspace string) ([]string, error) {
	if workspace == "" {
		return []string{}, nil
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, dir := range []string{".agents", filepath.Join(".agents", "skills")} {
		if err := root.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			return nil, errors.New("Bot skill directory is not a regular directory")
		}
	}
	return []string{filepath.Join(workspace, ".agents", "skills")}, nil
}
