package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This CLI is a local test harness, not an authenticated external event ingress.
// An installed host must obtain presence and source identity from its adapters.
func replay(ruleFile, eventFile string) error {
	b, err := os.ReadFile(ruleFile)
	if err != nil {
		return err
	}
	if len(b) > 256<<10 {
		return fmt.Errorf("rule file too large")
	}
	var rules []Rule
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&rules); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "activation-replay-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	e, err := newEngine(filepath.Join(dir, "ledger.json"), rules)
	if err != nil {
		return err
	}
	var input io.Reader = os.Stdin
	if eventFile != "" {
		f, err := os.Open(eventFile)
		if err != nil {
			return err
		}
		defer f.Close()
		input = f
	}
	s := bufio.NewScanner(input)
	s.Buffer(make([]byte, 4096), 20000)
	enc := json.NewEncoder(os.Stdout)
	for s.Scan() {
		var envelope struct {
			Event    *Event   `json:"event"`
			Presence Presence `json:"presence"`
		}
		if err = json.Unmarshal(s.Bytes(), &envelope); err != nil {
			return err
		}
		if envelope.Event != nil {
			if _, err = e.receive(context.Background(), *envelope.Event, time.Now()); err != nil {
				return err
			}
		}
		_, err = e.deliver(time.Now(), envelope.Presence, func(a Activation) error {
			return enc.Encode(map[string]any{"activation": a.ID, "rule": a.Rule, "effect": "would submit configured prompt", "fixture": true})
		})
		if err != nil {
			return err
		}
	}
	return s.Err()
}
