package caelis

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const workspaceTrustPath = "/configuration/workspace-trust"

// defaultNotebookTrust uses the Host's trust decision and configuration revision.
// Only the application-provided Notebook is eligible for this default.
func (s *Session) defaultNotebookTrust(ctx context.Context, host *client) error {
	if s.retainedWorkers || s.tools == nil || s.tools.NotebookDirectory == "" {
		return nil
	}
	path := s.tools.NotebookDirectory
	info, err := os.Lstat(path)
	if err != nil || !filepath.IsAbs(path) || !info.IsDir() {
		return errors.New("Notebook trust path unavailable")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("Notebook trust path unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// An ambiguous write can only be reconciled with its original receipt.
	s.mu.Lock()
	var pending string
	for id, op := range s.state.Operations {
		if op.Path == workspaceTrustPath && op.Outcome == "unknown" {
			pending = id
			break
		}
	}
	s.mu.Unlock()
	if pending != "" {
		return s.recoverOperation(ctx, pending)
	}

	var status wire.StatusSnapshot
	if err := host.json(ctx, "GET", "/status?cwd="+url.QueryEscape(path), nil, &status, "", ""); err != nil {
		return err
	}
	config := status.Configuration
	if config.WorkspaceTrust != wire.WorkspaceTrustLevelUnknown || config.Revision == "" {
		return nil
	}
	op := "notebook-trust-" + digest([]byte(path+"\x00"+string(config.Revision)))
	result, err := s.command(ctx, op, workspaceTrustPath, wire.WorkspaceTrustRequest{
		OperationId: &op, ExpectedRevision: &config.Revision,
		Cwd: path, WorkspaceKey: path, TrustLevel: wire.WorkspaceTrustLevelTrusted,
	})
	if err != nil {
		return err
	}
	if !succeeded(result.Outcome) {
		return errors.New("Notebook trust default unconfirmed")
	}
	return nil
}
