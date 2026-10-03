package machines

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) SSHConfigHosts() ([]string, error) { return configuredSSHHosts() }

func configuredSSHHosts() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("ssh_config_read_failed")
	}
	return readSSHHosts(home, filepath.Join(home, ".ssh", "config"), "/etc/ssh/ssh_config")
}

// This is an inventory of literal Host names, not an SSH configuration evaluator.
// Includes are bounded and read-only; Match/Host defaults, identities and routes
// are evaluated by native ssh -G only after the user chooses a connection.
func readSSHHosts(home string, roots ...string) ([]string, error) {
	hosts := []string{}
	seenFiles, seenHosts := map[string]bool{}, map[string]bool{}
	budget := int64(8 << 20)
	var read func(string, string, int) error
	read = func(path, base string, depth int) error {
		canonical, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if seenFiles[canonical] {
			return nil
		}
		if depth > 16 || len(seenFiles) >= 128 {
			return errors.New("SSH include limit")
		}
		seenFiles[canonical] = true
		file, err := os.Open(canonical)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 || info.Size() > budget {
			return errors.New("SSH file limit")
		}
		limit := min(budget, 2<<20)
		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil || int64(len(data)) > limit {
			return errors.New("SSH file limit")
		}
		budget -= int64(len(data))
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			fields := sshConfigFields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			switch strings.ToLower(fields[0]) {
			case "host":
				for _, host := range fields[1:] {
					key := strings.ToLower(host)
					if strings.ContainsAny(host, "*?![]") || seenHosts[key] || validate(api.MachineInput{Address: host, Port: 22, Authentication: "agent"}) != nil {
						continue
					}
					seenHosts[key] = true
					if len(hosts) >= 2048 {
						return errors.New("SSH host limit")
					}
					hosts = append(hosts, host)
				}
			case "include":
				for _, pattern := range fields[1:] {
					if strings.HasPrefix(pattern, "~/") {
						pattern = filepath.Join(home, pattern[2:])
					} else if !filepath.IsAbs(pattern) {
						pattern = filepath.Join(base, pattern)
					}
					paths, err := filepath.Glob(pattern)
					if err != nil {
						return err
					}
					for _, included := range paths {
						if err := read(included, base, depth+1); err != nil {
							return err
						}
					}
				}
			}
		}
		return scanner.Err()
	}
	for _, path := range roots {
		if err := read(path, filepath.Dir(path), 0); err != nil {
			// Do not return paths or private config contents to the renderer/log.
			return nil, errors.New("ssh_config_read_failed")
		}
	}
	slices.SortFunc(hosts, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return hosts, nil
}

// OpenSSH accepts whitespace or '=' after a directive, quoted arguments and
// escaped characters. Only Host and Include values are consumed by the inventory.
func sshConfigFields(line string) []string {
	fields := []string{}
	var word strings.Builder
	quoted, escaped := false, false
	flush := func() {
		if word.Len() != 0 {
			fields = append(fields, word.String())
			word.Reset()
		}
	}
	for _, c := range line {
		if escaped {
			word.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
			continue
		}
		if !quoted && c == '#' {
			break
		}
		if !quoted && (c == ' ' || c == '\t' || c == '=' && (len(fields) == 0 || len(fields) == 1 && word.Len() == 0)) {
			flush()
			continue
		}
		word.WriteRune(c)
	}
	if quoted || escaped {
		return nil
	}
	flush()
	return fields
}
