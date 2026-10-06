package caelis

import "github.com/caelis-labs/caelis-bot/internal/diagnosticlog"

// A watcher, callback or Worker fault is not evidence that the Runtime owner
// disappeared. Its original cursor/receipt remains fenced and retries locally.
func (s *Session) componentError(component, sid string, err error) {
	if err == nil {
		return
	}
	s.diagnostics.Write(diagnosticlog.Record{Level: "warning", Component: "caelis", Code: "component_recovering", Method: component, Thread: sid, Reason: diagnosticlog.Reason(err.Error())})
}
