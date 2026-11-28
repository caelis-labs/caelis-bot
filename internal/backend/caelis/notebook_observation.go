package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const shellExecutionCapability = "shell-execution-v1"

type notebookObservationFailure string

func (e notebookObservationFailure) Error() string        { return string(e) }
func (notebookObservationFailure) ConfirmedFailure() bool { return true }

// NotebookObserver uses the local managed Host's ordinary credential. Its
// workspace and Store are selected by application assembly, outside tool input.
type NotebookObserver struct {
	settings  api.RuntimeSettings
	directory string
}

func NewNotebookObserver(settings api.RuntimeSettings, directory string) *NotebookObserver {
	return &NotebookObserver{settings: settings, directory: directory}
}

func (o *NotebookObserver) connect(ctx context.Context) (*client, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	unavailable := notebookObservationFailure("managed local Core does not offer Shell observation")
	if !filepath.IsAbs(o.directory) {
		return nil, unavailable
	}
	d, token, err := Discover(o.settings)
	if err != nil {
		return nil, unavailable
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		return nil, unavailable
	}
	c.http.Transport.(*http.Transport).ResponseHeaderTimeout = 22 * time.Second
	info, err := initialize(ctx, c)
	if err != nil || value(info.InstanceId) != d.InstanceID || !slices.Contains(info.Capabilities, shellExecutionCapability) {
		c.http.CloseIdleConnections()
		return nil, unavailable
	}
	return c, nil
}

func (o *NotebookObserver) ShellObservationAvailable(ctx context.Context) bool {
	c, err := o.connect(ctx)
	if err != nil {
		return false
	}
	c.http.CloseIdleConnections()
	return true
}

func (o *NotebookObserver) ObserveShell(ctx context.Context, command string) (json.RawMessage, error) {
	c, err := o.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.http.CloseIdleConnections()

	req := wire.ShellRequest{Workspace: o.directory, Command: command}
	result := wire.ShellResult{ExitCode: -1}
	if err := c.json(ctx, http.MethodPost, "/shell/execute", req, &result, "", ""); err != nil {
		var remote *remoteError
		if errors.As(err, &remote) {
			switch remote.Status {
			case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
				http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
				return nil, notebookObservationFailure("Shell observation did not produce a usable sample")
			}
		}
		// This synchronous API has no durable operation key. A lost result
		// cannot be reconciled by running the same command again.
		return nil, errors.New("Shell observation result unconfirmed")
	}
	if result.Location != wire.ShellLocationSandbox || result.ExitCode < 0 {
		return nil, errors.New("Shell observation result unconfirmed")
	}
	if result.ExitCode != 0 {
		return nil, notebookObservationFailure("Shell observation exited unsuccessfully")
	}
	return json.RawMessage(result.Stdout), nil
}
