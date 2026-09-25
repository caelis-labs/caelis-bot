package taskterminal

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"unicode"
)

// CustomArgs parses an argv template, not shell code. The generated path is
// substituted after parsing and must occupy exactly one complete argument.
func CustomArgs(command, script string) ([]string, error) {
	if len(command) > 4096 || strings.ContainsRune(command, 0) {
		return nil, errors.New("invalid custom terminal command")
	}
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range command {
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteRune(r)
		started = true
	}
	if quote != 0 || escaped {
		return nil, errors.New("unfinished custom terminal quoting")
	}
	if started {
		args = append(args, word.String())
	}
	if len(args) < 2 || args[0] == "" {
		return nil, errors.New("custom terminal requires an executable and {script} argument")
	}
	count := 0
	for i, a := range args {
		if strings.Contains(a, "{script}") {
			if i == 0 || a != "{script}" {
				return nil, errors.New("{script} must be a separate argument")
			}
			count++
			args[i] = script
		}
	}
	if count != 1 {
		return nil, errors.New("custom terminal requires exactly one {script} argument")
	}
	return args, nil
}

func LaunchCustom(ctx context.Context, command, script string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	args, err := CustomArgs(command, script)
	if err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	if err = cmd.Start(); err != nil {
		return err
	}
	// Some terminal CLIs stay alive until the terminal closes. Confirmation is
	// observed by Launcher, independently of the terminal process lifetime.
	go func() { _ = cmd.Wait() }()
	return nil
}
