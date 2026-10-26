package machines

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type originalTerminalRuntime struct {
	inert
	opened []string
	target api.TerminalTarget
	err    error
}

func (f *originalTerminalRuntime) WorkTerminal(_ context.Context, id string) (api.TerminalTarget, error) {
	f.opened = append(f.opened, id)
	return f.target, f.err
}

func TestWorkTerminalKeepsOriginalLocalOwnerAndRejectsPeerFallback(t *testing.T) {
	for _, runtime := range []string{"codex", "caelis"} {
		t.Run(runtime, func(t *testing.T) {
			want := api.TerminalTarget{Runtime: runtime, Thread: "original-thread", Session: "original-session", Endpoint: "unix:///original-owner", Directory: t.TempDir()}
			local := &originalTerminalRuntime{target: want}
			s, err := Open(t.TempDir(), local)
			if err != nil {
				t.Fatal(err)
			}
			s.state.Routes["peer-task"] = "original-peer"
			got, err := s.WorkTerminal(t.Context(), "original-local-task")
			if err != nil || got != want || len(local.opened) != 1 || local.opened[0] != "original-local-task" {
				t.Fatalf("native terminal lost its original binding: %+v %v", got, err)
			}
			if _, err = s.WorkTerminal(t.Context(), "peer-task"); err == nil || len(local.opened) != 1 {
				t.Fatal("remote task fell back to a local terminal", err)
			}
			cause := errors.New("original terminal unavailable")
			local.err = cause
			if _, err = s.WorkTerminal(t.Context(), "original-local-task"); !errors.Is(err, cause) {
				t.Fatal("terminal failure changed the owner", err)
			}
		})
	}
}
