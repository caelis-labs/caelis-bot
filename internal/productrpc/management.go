package productrpc

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

func managementScope(s Scope) productmanagement.Scope {
	return productmanagement.Scope{BotID: s.BotID, Generation: s.Generation}
}
func managementPath(path string) bool {
	switch path {
	case "/v1/management/nodes", "/v1/management/capabilities", "/v1/management/releases", "/v1/management/status", "/v1/management/configuration", "/v1/management/resolve", "/v1/management/execution":
		return true
	}
	return false
}
func validRuntimeManagement(c productmanagement.RuntimeCommand, resolve bool) bool {
	if !identifier.MatchString(c.ID) || (c.Runtime != "codex" && c.Runtime != "caelis") || !publicManagementText(c.Version, 128) || !publicManagementText(c.ExpectedVersion, 128) {
		return false
	}
	switch c.Action {
	case "detect", "check-update":
		return !resolve && c.Version == "" && c.ExpectedVersion == ""
	case "install":
		return !resolve && c.Version != "" && c.ExpectedVersion == ""
	case "update":
		return !resolve && c.Version != "" && c.ExpectedVersion != ""
	case "resolve":
		return resolve && c.Version != ""
	}
	return false
}
func publicManagementText(v string, n int) bool {
	return len(v) <= n && !strings.ContainsAny(v, "\x00\r\n")
}
func validConfiguration(c api.RuntimeConfigurationChange) bool {
	if len(c.ExpectedRevision) == 0 || len(c.ExpectedRevision) > 20 {
		return false
	}
	if _, err := strconv.ParseUint(c.ExpectedRevision, 10, 64); err != nil {
		return false
	}
	if !publicManagementText(c.ID, 256) || !publicManagementText(c.Name, 256) || len(c.Description) > 8192 || strings.ContainsRune(c.Description, '\x00') || !publicManagementText(c.Selection.Model, 512) || !publicManagementText(c.Selection.Effort, 64) || !publicManagementText(c.Selection.ServiceTier, 64) {
		return false
	}
	empty := c.Selection == (api.WorkExecutionSettings{})
	switch c.Action {
	case "main":
		return c.Selection.Model != "" && c.ID == "" && c.Name == "" && c.Description == ""
	case "bind":
		return c.ID != "" && c.Selection.Model != "" && c.Name == "" && c.Description == ""
	case "reset", "delete-role", "remove-model", "disconnect-agent":
		return c.ID != "" && c.Name == "" && c.Description == "" && empty
	case "create-role":
		return c.ID != "" && c.Name == "" && empty
	case "save-set", "apply-set", "delete-set":
		return c.Name != "" && c.ID == "" && c.Description == "" && empty
	}
	return false
}
func receiptOutcome(v string) bool { return v == "accepted" || v == "rejected" || v == "unknown" }

func (s *Server) manageHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/management/nodes" {
		s.nodesHTTP(w, r)
		return
	}
	if r.URL.Path == "/v1/management/resolve" {
		var command productmanagement.RuntimeCommand
		if !decode(w, r, &command) || !s.scope(w, Scope{command.BotID, command.Generation}) {
			return
		}
		if !validRuntimeManagement(command, true) {
			problem(w, 400, "invalid-management-command")
			return
		}
		s.resolveRuntime(w, command)
		return
	}
	var query struct {
		Scope
		Runtime string `json:"runtime,omitempty"`
	}
	if !decode(w, r, &query) || !s.scope(w, query.Scope) {
		return
	}
	if (r.URL.Path == "/v1/management/status" && query.Runtime != "codex" && query.Runtime != "caelis") || (r.URL.Path != "/v1/management/status" && query.Runtime != "") {
		problem(w, 400, "invalid-management-query")
		return
	}
	if r.URL.Path == "/v1/management/capabilities" {
		caps := productmanagement.Capabilities{}
		if s.management != nil {
			caps = s.management.Capabilities()
			caps.ConfigurationReceiptLookup = false
		}
		caps.Execution = s.execution != nil
		s.write(w, caps)
		return
	}
	if r.URL.Path == "/v1/management/execution" {
		s.readExecution(w, r, query.Scope)
		return
	}
	if s.management == nil {
		problem(w, 409, "management-unavailable")
		return
	}
	scope := managementScope(query.Scope)
	switch r.URL.Path {
	case "/v1/management/releases":
		v, err := s.management.ReviewedReleases(scope)
		if err != nil {
			problem(w, 409, "management-unavailable")
			return
		}
		s.write(w, v)
	case "/v1/management/status":
		v, err := s.management.RuntimeStatus(r.Context(), scope, query.Runtime)
		if err != nil {
			problem(w, 409, "management-unavailable")
			return
		}
		if v.Runtime != query.Runtime {
			problem(w, 502, "invalid-management-status")
			return
		}
		v.RequestID = ""
		s.write(w, v)
	case "/v1/management/configuration":
		v, err := s.management.RuntimeConfiguration(r.Context(), scope)
		if err != nil {
			problem(w, 409, "configuration-unavailable")
			return
		}
		s.write(w, v)
	}
}
func (s *Server) executeManagement(ctx context.Context, c Command) Result {
	r := Result{ID: c.ID, Outcome: "rejected", Code: "management-unavailable"}
	if s.management == nil {
		return r
	}
	caps := s.management.Capabilities()
	if c.RuntimeManagement != nil {
		if !caps.Installation {
			return r
		}
		v, err := s.management.ManageRuntime(ctx, *c.RuntimeManagement)
		if err != nil || v.Scope != c.RuntimeManagement.Scope || v.ID != c.ID || !receiptOutcome(v.Outcome) {
			r.Outcome, r.Code = "unknown", "invalid-management-receipt"
			return r
		}
		if v.Status.RequestID != "" && v.Status.RequestID != c.ID {
			r.Outcome, r.Code = "unknown", "invalid-management-receipt"
			return r
		}
		r.RuntimeManagement = &v
		r.Outcome, r.Code = v.Outcome, v.Code
		return r
	}
	if !caps.Configuration {
		return r
	}
	v, err := s.management.ChangeRuntimeConfiguration(ctx, *c.Configuration)
	if err != nil || v.Scope != c.Configuration.Scope || v.ID != c.ID || !receiptOutcome(v.Outcome) {
		r.Outcome, r.Code = "unknown", "invalid-management-receipt"
		return r
	}
	// Core operation IDs belong to the native boundary. Product receipts retain
	// only the stable business ID/digest and outcome, without native IDs or messages.
	v.Native.OperationID = ""
	if len(v.Native.Message) > 4096 {
		v.Native.Message = "Configuration operation outcome received"
	}
	r.Configuration = &v
	r.Outcome, r.Code = v.Outcome, v.Code
	return r
}

// resolve is only reconciliation of a durable original installation intent. It
// cannot reserve a new ID, adopt an earlier generation, or install/update again.
func (s *Server) resolveRuntime(w http.ResponseWriter, c productmanagement.RuntimeCommand) {
	s.commands.Lock()
	defer s.commands.Unlock()
	s.journal.mu.Lock()
	entry, ok := s.journal.doc.Entries[c.ID]
	s.journal.mu.Unlock()
	if !ok || entry.Runtime == nil {
		problem(w, 409, "original-installation-unavailable")
		return
	}
	original := *entry.Runtime
	if c.Runtime != original.Runtime || c.Version != original.Version || c.ExpectedVersion != original.ExpectedVersion {
		problem(w, 409, "original-installation-conflict")
		return
	}
	result := entry.Result
	if result.Outcome != "unknown" || result.Code == "pending" {
		s.write(w, result)
		return
	}
	if s.stopping || s.management == nil || original.Scope != managementScope(s.identity.Scope) {
		result.Code = "original-scope-unavailable"
		if result.RuntimeManagement != nil {
			v := *result.RuntimeManagement
			v.Code = result.Code
			result.RuntimeManagement = &v
		}
		s.write(w, result)
		return
	}
	// The original reservation is already durable. Observer cancellation cannot
	// cancel this admitted lookup, and a lookup never invokes native installation.
	ctx, cancel := context.WithTimeout(s.opts.Context, 2*time.Minute)
	defer cancel()
	original.Action = "resolve"
	result = s.executeManagement(ctx, Command{Scope: s.identity.Scope, ID: c.ID, Kind: "manage-runtime", RuntimeManagement: &original})
	if s.journal.finish(c.ID, result) != nil {
		result = Result{ID: c.ID, Outcome: "unknown", Code: "receipt-save-failed"}
	}
	s.write(w, result)
}

func (c *Client) managementQuery(ctx context.Context, path, provider string, out any) error {
	scope, err := c.scope()
	if err != nil {
		return err
	}
	return c.request(ctx, "POST", path, struct {
		Scope
		Runtime string `json:"runtime,omitempty"`
	}{scope, provider}, out)
}
func (c *Client) ManagementCapabilities(ctx context.Context) (productmanagement.Capabilities, error) {
	var v productmanagement.Capabilities
	err := c.managementQuery(ctx, "/v1/management/capabilities", "", &v)
	return v, err
}
func (c *Client) ReviewedReleases(ctx context.Context) ([]productmanagement.ReviewedRelease, error) {
	var v []productmanagement.ReviewedRelease
	err := c.managementQuery(ctx, "/v1/management/releases", "", &v)
	return v, err
}
func (c *Client) RuntimeStatus(ctx context.Context, provider string) (runtimemanagement.Status, error) {
	var v runtimemanagement.Status
	err := c.managementQuery(ctx, "/v1/management/status", provider, &v)
	return v, err
}
func (c *Client) RuntimeConfiguration(ctx context.Context) (api.RuntimeConfiguration, error) {
	var v api.RuntimeConfiguration
	err := c.managementQuery(ctx, "/v1/management/configuration", "", &v)
	return v, err
}
func (c *Client) ManageRuntime(ctx context.Context, in productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
	scope, err := c.scope()
	if err != nil {
		return productmanagement.RuntimeResult{}, err
	}
	in.Scope = managementScope(scope)
	unknown := productmanagement.RuntimeResult{Scope: in.Scope, ID: in.ID, Outcome: "unknown", Code: "response-unobserved"}
	var r Result
	if in.Action == "resolve" {
		r = Result{ID: in.ID, Outcome: "unknown", Code: "response-unobserved"}
		err = c.request(ctx, "POST", "/v1/management/resolve", in, &r)
	} else {
		r, err = c.Command(ctx, Command{ID: in.ID, Kind: "manage-runtime", RuntimeManagement: &in})
	}
	if err != nil {
		return unknown, err
	}
	if r.ID != in.ID {
		return unknown, errors.New("management receipt identity mismatch")
	}
	if r.RuntimeManagement == nil {
		unknown.Outcome, unknown.Code = r.Outcome, r.Code
		return unknown, nil
	}
	v := *r.RuntimeManagement
	if v.ID != in.ID || v.BotID != scope.BotID || !receiptOutcome(v.Outcome) {
		return unknown, errors.New("management receipt identity mismatch")
	}
	return v, nil
}
func (c *Client) ChangeRuntimeConfiguration(ctx context.Context, in productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error) {
	scope, err := c.scope()
	if err != nil {
		return productmanagement.ConfigurationResult{}, err
	}
	in.Scope = managementScope(scope)
	unknown := productmanagement.ConfigurationResult{Scope: in.Scope, ID: in.ID, Outcome: "unknown", Code: "response-unobserved"}
	r, err := c.Command(ctx, Command{ID: in.ID, Kind: "configure-runtime", Configuration: &in})
	if err != nil {
		return unknown, err
	}
	if r.Configuration == nil {
		unknown.Outcome, unknown.Code = r.Outcome, r.Code
		return unknown, nil
	}
	v := *r.Configuration
	if v.Scope != in.Scope || v.ID != in.ID || !receiptOutcome(v.Outcome) {
		return unknown, errors.New("configuration receipt identity mismatch")
	}
	return v, nil
}
