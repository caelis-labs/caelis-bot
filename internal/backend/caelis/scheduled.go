package caelis

import (
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend/activation"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Background turns are correlated through native command targets. Safe-point
// input IDs additionally classify inputs; prose never establishes provenance.
func (s *Session) applyScheduledEnvelope(v *view, e wire.Envelope, scope inputScope) {
	if v.WorkerOutput == nil && scope.SessionID != "" {
		for _, worker := range s.state.Workers {
			if worker.Binding.SessionId == scope.SessionID {
				v.WorkerOutput = newWorkerTranscript(scope.SessionID, v.Items)
				break
			}
		}
	}
	turn := value(e.TurnId)
	id := value(e.InputOperationId)
	j := s.state.Operations[id]
	scheduled := turn != "" && j.Scheduled
	hostNotice := j.hostNotice()
	if !hostNotice && id == "" && turn != "" {
		for _, candidate := range s.state.Operations {
			if candidate.TurnID == turn && candidate.hostNotice() {
				hostNotice = true
				break
			}
		}
	}
	if scheduled {
		j.TurnID = turn
		s.state.Operations[id] = j
	}
	applyScopedEnvelope(v, e, scope, scheduled, hostNotice)
	if v.WorkerOutput != nil && v.WorkerOutput.Session == scope.SessionID {
		v.WorkerOutput.observeItems(v.Items)
	}
	if turn != "" && e.Lifecycle != nil && e.Kind == "caelis/lifecycle" && value(e.ApprovalRequestId) == "" && (value(e.Scope) == "" || value(e.Scope) == "main") {
		if v.Turns == nil {
			v.Turns = map[string]string{}
		}
		v.Turns[turn] = e.Lifecycle.State
	}
}
func (s *Session) presentScheduled(out api.Snapshot, v *view) api.Snapshot {
	turns := map[string]string{}
	dreams := map[string]string{}
	for id, j := range s.state.Operations {
		if j.hostNotice() {
			path := "/sessions/" + idPath(s.state.Session.SessionId)
			if out.RuntimeOwner != "" && j.TurnID != "" && succeeded(wire.Outcome(j.Outcome)) && (j.Path == path+"/prompt" || j.Path == path+"/steer") {
				phase := v.Turns[j.TurnID]
				if phase == "cancelled" {
					phase = "interrupted"
				}
				out.ReportSources = append(out.ReportSources, api.ReportSource{RuntimeOwner: out.RuntimeOwner, TurnKey: j.TurnID, ReportID: id, Phase: phase})
			}
			for index := range out.Items {
				item := &out.Items[index]
				if item.Kind == "user" && item.RequestID == id {
					item.Kind = "hostNotice"
				}
			}
		}
		if j.Scheduled && j.TurnID != "" {
			status := ""
			for sid, history := range s.state.Views {
				if j.Path == "/sessions/"+idPath(sid)+"/prompt" {
					status = history.Turns[j.TurnID]
				}
			}
			if j.TurnID == out.CurrentTurn {
				status = value(v.State.Run.Status)
			}
			turns[j.TurnID] = status
			if status == "cancelled" {
				status = "interrupted"
			}
			if !j.Dream && succeeded(wire.Outcome(j.Outcome)) {
				for _, request := range j.OriginRequests {
					out.ScheduledSources = append(out.ScheduledSources, api.ScheduledSource{RuntimeOwner: j.OriginOwner, TurnKey: j.TurnID, RequestID: request, Phase: status})
				}
			}
			if j.Dream {
				dreams[j.TurnID] = status
			}
		}
	}
	sort.Slice(out.ReportSources, func(i, j int) bool { return out.ReportSources[i].ReportID < out.ReportSources[j].ReportID })
	pending := s.sendingScheduled != ""
	if pending && out.CurrentTurn != s.scheduledPreviousTurn {
		turns[out.CurrentTurn] = "running"
	}
	return activation.Dream(activation.Present(out, turns, pending), dreams, pending && s.state.Operations[s.sendingScheduled].Dream)
}
