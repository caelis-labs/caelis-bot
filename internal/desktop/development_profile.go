package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

// One development-default profile selector. Explicit test roots always win;
// release/onboarding never read it. No accounts, routing or task state lives here.
type developmentProfile struct {
	DataDirectory string `json:"dataDirectory"`
	CodexHome     string `json:"codexHome,omitempty"`
}

func developmentDefault(base string) (developmentProfile, error) {
	if buildChannel != "development" {
		return developmentProfile{}, nil
	}
	path := filepath.Join(base, "Caelis Bot Dev", "active-profile.json")
	f, e := openDevelopmentProfile(path)
	if os.IsNotExist(e) {
		return developmentProfile{}, nil
	}
	if e != nil {
		return developmentProfile{}, errors.New("development profile unavailable")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return developmentProfile{}, errors.New("development profile invalid")
	}
	var v developmentProfile
	if json.NewDecoder(f).Decode(&v) != nil || !filepath.IsAbs(v.DataDirectory) || v.CodexHome != "" && !filepath.IsAbs(v.CodexHome) {
		return developmentProfile{}, errors.New("development profile invalid")
	}
	for _, path := range []string{v.DataDirectory, v.CodexHome} {
		if path != "" {
			if info, e := os.Stat(path); e != nil || !info.IsDir() {
				return developmentProfile{}, errors.New("development profile directory unavailable")
			}
		}
	}
	return v, nil
}

// Apply the chosen private Runtime home before shell-environment recovery can
// supply a different account home. Explicit isolation always remains authoritative.
func prepareDevelopmentProfileEnvironment() error {
	if buildChannel != "development" {
		return nil
	}
	if _, set := os.LookupEnv("CAELIS_BOT_DATA_DIR"); set {
		return nil
	}
	base, e := developmentProfileBase()
	if e != nil {
		return e
	}
	v, e := developmentDefault(base)
	if e != nil || v.DataDirectory == "" {
		return e
	}
	return applyDevelopmentProfileEnvironment(v)
}

func developmentProfileBase() (string, error) {
	// Selection precedes shell recovery and belongs to the OS account, even
	// when a launcher has omitted HOME or inherited a legacy Notebook HOME.
	if runtime.GOOS == "darwin" {
		account, err := user.Current()
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(account.HomeDir) {
			return "", errors.New("development account home unavailable")
		}
		return filepath.Join(account.HomeDir, "Library", "Application Support"), nil
	}
	return nativeDataDirectoryBase()
}
func applyDevelopmentProfileEnvironment(v developmentProfile) error {
	if _, set := os.LookupEnv("CAELIS_BOT_DATA_DIR"); set {
		return nil
	}
	if e := os.Setenv("CAELIS_BOT_DATA_DIR", v.DataDirectory); e != nil {
		return e
	}
	if os.Getenv("CODEX_HOME") == "" && v.CodexHome != "" {
		return os.Setenv("CODEX_HOME", v.CodexHome)
	}
	return nil
}
