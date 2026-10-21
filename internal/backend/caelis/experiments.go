package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

const experimentalCapability = "experimental-features-v1"

var experimentalIDs = []string{"async_compact", "mcp_catalog_refresh", "deferred_tool_retirement"}

// Only local journal transitions are serialized, separately for each Store's
// journal. No lock spans a Host request. Core CAS remains the shared config owner.
var experimentJournalLocks sync.Map

func lockExperimentJournal(ctx context.Context, path string) (func(), error) {
	mu, _ := experimentJournalLocks.LoadOrStore(filepath.Clean(path), &sync.Mutex{})
	lock := mu.(*sync.Mutex)
	if err := lockwait.Lock(ctx, lock); err != nil {
		return nil, err
	}
	return lock.Unlock, nil
}

func readExperimentState(ctx context.Context, path string) (experimentJournal, error) {
	unlock, err := lockExperimentJournal(ctx, path)
	if err != nil {
		return experimentJournal{}, err
	}
	defer unlock()
	return readExperimentJournal(path)
}

type experimentJournal struct {
	Request *wire.ExperimentalRequest `json:"request,omitempty"`
	Outcome string                    `json:"outcome,omitempty"`
}

func experimentPath(dir string, info wire.ServerInfo) string {
	return filepath.Join(dir, "experiments-"+digest([]byte(value(info.StoreId)))[:24]+".json")
}
func readExperimentJournal(path string) (experimentJournal, error) {
	var j experimentJournal
	raw, err := privateRead(path, 16384)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	if json.Unmarshal(raw, &j) != nil || j.Request != nil && j.Request.OperationId == "" || j.Request == nil && j.Outcome != "" {
		return j, errors.New("experimental save record unavailable")
	}
	return j, nil
}
func experimentalClient(ctx context.Context, settings api.RuntimeSettings) (*client, wire.ServerInfo, error) {
	d, token, err := Discover(settings)
	if err != nil {
		return nil, wire.ServerInfo{}, err
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		return nil, wire.ServerInfo{}, err
	}
	info, err := initialize(ctx, c)
	if err == nil && value(info.InstanceId) != d.InstanceID {
		err = errors.New("Caelis host changed")
	}
	if err != nil {
		c.http.CloseIdleConnections()
		return nil, info, err
	}
	return c, info, nil
}
func experimentalSnapshot(ctx context.Context, c *client) (wire.ExperimentalSnapshot, error) {
	var v wire.ExperimentalSnapshot
	err := c.json(ctx, "GET", "/configuration/experimental", nil, &v, "", "")
	if err != nil {
		return v, err
	}
	if _, err = strconv.ParseUint(string(v.Revision), 10, 64); err != nil || v.Scope != "host" || v.AppliesAt != "next_runtime_activation" || v.Desired.AsyncCompact == nil || v.Desired.McpCatalogRefresh == nil || v.Desired.DeferredToolRetirement == nil {
		return v, errors.New("experimental configuration unavailable")
	}
	for _, id := range experimentalIDs {
		if !slices.ContainsFunc(v.Features, func(f wire.ExperimentalDefinition) bool { return f.Id == id }) {
			return v, errors.New("experimental catalog unavailable")
		}
	}
	return v, nil
}
func experimentValues(v wire.ExperimentalValues) map[string]*bool {
	return map[string]*bool{"async_compact": v.AsyncCompact, "mcp_catalog_refresh": v.McpCatalogRefresh, "deferred_tool_retirement": v.DeferredToolRetirement}
}
func projectExperiments(v wire.ExperimentalSnapshot, j experimentJournal) api.RuntimeExperiments {
	out := api.RuntimeExperiments{Supported: true, Revision: string(v.Revision), Features: []api.RuntimeExperiment{}, SaveState: j.Outcome}
	values := experimentValues(v.Desired)
	for _, id := range experimentalIDs {
		out.Features = append(out.Features, api.RuntimeExperiment{ID: id, Enabled: *values[id]})
	}
	if j.Request != nil {
		out.OperationID = j.Request.OperationId
	}
	if j.Outcome == "committed" {
		out.SaveState = "saved"
		for id, want := range experimentValues(j.Request.Values) {
			if want != nil && *values[id] != *want {
				out.SaveState = "superseded"
			}
		}
	}
	return out
}
func settleExperiment(ctx context.Context, c *client, path string, j *experimentJournal) error {
	if j.Request == nil || j.Outcome != "pending" {
		return nil
	}
	var receipt wire.CommandResult
	if err := c.json(ctx, "GET", "/operations/"+idPath(j.Request.OperationId), nil, &receipt, "", ""); err != nil {
		return nil
	} // Unknown stays unknown, including missing receipts.
	return recordExperiment(ctx, path, j, receipt)
}
func recordExperiment(ctx context.Context, path string, j *experimentJournal, r wire.CommandResult) error {
	if j.Request == nil || r.OperationId != j.Request.OperationId {
		return nil
	}
	switch r.Outcome {
	case "committed", "rejected", "conflicted":
		next := *j
		next.Outcome = string(r.Outcome)
		unlock, err := lockExperimentJournal(ctx, path)
		if err != nil {
			return err
		}
		defer unlock()
		current, err := readExperimentJournal(path)
		if err != nil {
			return err
		}
		// A delayed reply may only settle its own original intent. Never replace
		// a newer user action that has already been admitted by another caller.
		if current.Request != nil && current.Request.OperationId == j.Request.OperationId && current.Outcome == "pending" {
			if err = privateWrite(path, next); err != nil {
				return err
			}
		}
		*j = next
	}
	return nil
}
func postExperiment(ctx context.Context, c *client, path string, j *experimentJournal) {
	var r wire.CommandResult
	err := c.json(ctx, "POST", "/configuration/experimental", j.Request, &r, j.Request.OperationId, string(j.Request.ExpectedRevision))
	if err != nil {
		var remote *remoteError
		if errors.As(err, &remote) && (remote.Code == "invalid_argument" || remote.Code == "unauthenticated" || remote.Code == "permission_denied" || remote.Code == "unsupported") {
			r = wire.CommandResult{OperationId: j.Request.OperationId, Outcome: "rejected"}
		}
	}
	// A failed final journal write leaves the pre-dispatch pending intent durable.
	_ = recordExperiment(ctx, path, j, r)
}

// ReadRuntimeExperiments only reflects Core configuration and original receipts.
func ReadRuntimeExperiments(ctx context.Context, settings api.RuntimeSettings, dir string) (api.RuntimeExperiments, error) {
	c, info, err := experimentalClient(ctx, settings)
	if err != nil {
		return api.RuntimeExperiments{}, err
	}
	defer c.http.CloseIdleConnections()
	if !slices.Contains(info.Capabilities, experimentalCapability) {
		return api.RuntimeExperiments{Features: []api.RuntimeExperiment{}}, nil
	}
	path := experimentPath(dir, info)
	j, err := readExperimentState(ctx, path)
	if err != nil {
		return api.RuntimeExperiments{}, err
	}
	if err = settleExperiment(ctx, c, path, &j); err != nil {
		return api.RuntimeExperiments{}, err
	}
	v, err := experimentalSnapshot(ctx, c)
	if err != nil {
		return api.RuntimeExperiments{}, err
	}
	return projectExperiments(v, j), nil
}

// ChangeRuntimeExperiments acknowledges a durable save flow. The caller reads
// that flow until saved/rejected/conflicted; a pending flow never posts again.
func ChangeRuntimeExperiments(ctx context.Context, settings api.RuntimeSettings, dir string, change api.RuntimeExperimentChange) (api.RuntimeExperiments, error) {
	reject := func() (api.RuntimeExperiments, error) {
		return api.RuntimeExperiments{}, errors.Join(&api.ActionRejectedError{Code: "experimental_save_unavailable"}, ctx.Err())
	}
	if !slices.Contains(experimentalIDs, change.ID) {
		return reject()
	}
	if _, err := strconv.ParseUint(change.ExpectedRevision, 10, 64); err != nil {
		return reject()
	}
	c, info, err := experimentalClient(ctx, settings)
	if err != nil {
		return reject()
	}
	defer c.http.CloseIdleConnections()
	if !slices.Contains(info.Capabilities, experimentalCapability) {
		return reject()
	}
	path := experimentPath(dir, info)
	j, err := readExperimentState(ctx, path)
	if err != nil {
		return reject()
	}
	if err = settleExperiment(ctx, c, path, &j); err != nil {
		return reject()
	}
	v, err := experimentalSnapshot(ctx, c)
	if err != nil {
		return reject()
	}
	unlock, err := lockExperimentJournal(ctx, path)
	if err != nil {
		return reject()
	}
	// Re-read after network work: another tab may have reserved an intent.
	j, err = readExperimentJournal(path)
	if err != nil {
		unlock()
		return reject()
	}
	if j.Outcome == "pending" {
		unlock()
		return projectExperiments(v, j), nil
	}
	// A stale tab's deliberate action must not overwrite a newer Host revision.
	if string(v.Revision) != change.ExpectedRevision {
		out := projectExperiments(v, j)
		out.SaveState = "conflicted"
		unlock()
		return out, nil
	}
	values := wire.ExperimentalValues{}
	switch change.ID {
	case "async_compact":
		values.AsyncCompact = &change.Enabled
	case "mcp_catalog_refresh":
		values.McpCatalogRefresh = &change.Enabled
	case "deferred_tool_retirement":
		values.DeferredToolRetirement = &change.Enabled
	}
	j = experimentJournal{Request: &wire.ExperimentalRequest{OperationId: "bot-experiment-" + rand.Text(), ExpectedRevision: v.Revision, Values: values}, Outcome: "pending"}
	err = privateWrite(path, j)
	unlock()
	if err != nil {
		return reject()
	}
	postExperiment(ctx, c, path, &j)
	if current, readErr := experimentalSnapshot(ctx, c); readErr == nil {
		return projectExperiments(current, j), nil
	}
	// Receipt commit is not UI readback success. Keep the original flow available.
	out := projectExperiments(v, j)
	out.SaveState = "pending"
	return out, nil
}
