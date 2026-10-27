package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"strings"
	"testing"
)

func TestPeerAutomaticDisplayNameDoesNotChangeOriginalPairIntent(t *testing.T) {
	target := newDualPeerTarget(t)
	s, e := Open(t.TempDir(), &dualPeerLocal{})
	if e != nil {
		t.Fatal(e)
	}
	input := api.BotConnectionInput{Address: target.http.URL}
	ref, e := s.PrepareBotConnection(input, nil)
	if e != nil {
		t.Fatal(e)
	}
	v, _, e := s.ConnectPreparedBot(t.Context(), ref, input)
	if e != nil {
		t.Fatal(e)
	}
	host, e := os.Hostname()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(v.Name, "bot-") || len(v.Name) != 10 || s.Machines()[0].Name != v.Name || v.RemoteName != host {
		t.Fatal("URL connection did not keep its short local identifier", v)
	}
	if s.state.Profiles[ref.ID].View.Name != input.Address {
		t.Fatal("display metadata changed persisted authorization intent")
	}
	if _, e = s.ReadBotConnectionReceipt(t.Context(), ref); e != nil {
		t.Fatal("automatic name trapped lost original reply", e)
	}
	// An explicit name is still user's display choice, not another peer identity.
	input.ID = ref.ID
	input.Name = "My worker"
	if _, e = s.ConnectBot(t.Context(), input); e != nil {
		t.Fatal(e)
	}
	if s.Machines()[0].Name != "My worker" {
		t.Fatal("remote name overrode user rename")
	}
}

func TestWorkerObservationUsesDispatchEnvironmentIdentifier(t *testing.T) {
	target := newDualPeerTarget(t)
	s, manager, _, machine, _ := dualPeerConnect(t, target, "")
	task, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "display-task", Title: "Original", Prompt: "run"})
	if err != nil {
		t.Fatal(err)
	}
	proof := s.state.Profiles[machine].Peer
	for _, name := range []string{s.TaskMachines()[0].Name, "fedora"} {
		if _, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: target.http.URL, Name: name}); err != nil {
			t.Fatal(err)
		}
		observation, err := s.ObserveWork(t.Context(), task.ID)
		if err != nil || observation.State.Task.MachineName != name || s.TaskMachines()[0].Name != name {
			t.Fatal("catalog and observation disagree", observation.State.Task, name, err)
		}
		observation.State.Task.MachineName = "untrusted peer label"
		p := s.state.Profiles[machine]
		s.applyPeerObservation(task.ID, p, s.state.Runtimes[task.ID], observation)
		if s.observations[task.ID].State.Task.MachineName != name {
			t.Fatal("pushed observation lost source label")
		}
		found := false
		for _, state := range s.WorkStates() {
			if state.Task.ID == task.ID {
				found = state.Task.MachineName == name && state.Task.Machine == machine
			}
		}
		if !found || s.state.Profiles[machine].Peer != proof || s.state.Routes[task.ID] != machine {
			t.Fatal("display change lost state or changed ownership")
		}
	}
}

func TestMachineIdentifiersAreUniqueAndPersistWithoutChangingPairIntent(t *testing.T) {
	s, err := Open(t.TempDir(), &dualPeerLocal{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.PrepareBotConnection(api.BotConnectionInput{Address: "http://127.0.0.1:19331"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := s.Machines()[0].Name
	_, err = s.PrepareBotConnection(api.BotConnectionInput{Address: "http://127.0.0.1:19332", Name: strings.ToUpper(name)}, nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate_bot_name") || len(s.state.Profiles) != 1 {
		t.Fatal("duplicate label admitted", err)
	}
	old := s.state.Profiles[first.ID]
	old.Label = "" // Previous releases retained URL as their pairing intent.
	s.state.Profiles[first.ID] = old
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.root, &dualPeerLocal{})
	if err != nil {
		t.Fatal(err)
	}
	current := reopened.state.Profiles[first.ID]
	if current.Label != name || current.View != old.View || current.Peer != old.Peer || reopened.botConnectionReference(current, first.Address).Configuration != first.Configuration {
		t.Fatal("display upgrade changed original intent", current)
	}
	again, err := Open(s.root, &dualPeerLocal{})
	if err != nil || again.Machines()[0].Name != name {
		t.Fatal("identifier changed on reopen", err)
	}
}

func TestSSHDefaultHostnameCollisionAndCustomOverride(t *testing.T) {
	s, err := Open(t.TempDir(), &dualPeerLocal{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareBotConnection(api.BotConnectionInput{Name: "build", Address: "http://127.0.0.1:19331"}, nil); err != nil {
		t.Fatal(err)
	}
	ref, err := s.PrepareBotConnection(api.BotConnectionInput{Address: "ssh://fedora:9317"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := s.state.Profiles[ref.ID]
	if machineView(p).Name != "fedora" || !p.LabelPending {
		t.Fatal("SSH alias missing before hostname is known")
	}
	p.View.RemoteName = "build"
	p = s.resolveSSHLabelLocked(p)
	if machineView(p).Name != "build-2" || p.LabelPending || p.View.Name != ref.Address {
		t.Fatal("SSH hostname collision changed pairing intent", p)
	}
	s.state.Profiles[ref.ID] = p
	renamed, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: ref.ID, Address: ref.Address, Name: "fedora"})
	if err != nil || renamed.Name != "fedora" || s.state.Profiles[ref.ID].LabelPending {
		t.Fatal("custom SSH label not preserved", renamed, err)
	}
}
