package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type selectedWorkers []api.WorkTarget

func (s *selectedWorkers) String() string {
	values := make([]string, 0, len(*s))
	for _, target := range *s {
		values = append(values, target.NodeID+"/"+target.Backend)
	}
	return strings.Join(values, ",")
}

var startupNodeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (s *selectedWorkers) Set(value string) error {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !startupNodeID.MatchString(parts[0]) || parts[0] == api.LocalNodeID || (parts[1] != "caelis" && parts[1] != "codex") {
		return errors.New("connect-worker requires exact NODE/caelis or NODE/codex")
	}
	target := api.WorkTarget{NodeID: parts[0], Backend: parts[1], Role: api.RoleWorker}
	for _, previous := range *s {
		if previous == target {
			return errors.New("connect-worker target repeated")
		}
	}
	if len(*s) >= 16 {
		return errors.New("too many selected Worker targets")
	}
	*s = append(*s, target)
	return nil
}

type residentOwner interface {
	PreparePersonal() error
	Start() error
	Close() error
}
type startupWorkers interface {
	WorkerNodes() backend.WorkerNodeSetup
	ConnectWorkerTarget(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error)
}

func startupTarget(config backend.WorkerNodeConfig) api.WorkTarget {
	driver := config.Backend
	if driver == "" {
		driver = "caelis"
	}
	return api.WorkTarget{NodeID: config.ID, Backend: driver, Role: api.RoleWorker}
}
func connectStartupWorkers(ctx context.Context, service startupWorkers, selected []api.WorkTarget) error {
	if len(selected) == 0 {
		return nil
	}
	snapshot := service.WorkerNodes()
	if snapshot.Issue != "" {
		return errors.New("selected Worker configuration is unavailable")
	}
	// Validate the whole selection before any connection. NodeID alone cannot
	// select one of two configured backends on the same machine.
	seen := map[api.WorkTarget]bool{}
	for _, target := range selected {
		if target.Role != api.RoleWorker || seen[target] {
			return errors.New("invalid or repeated exact Worker target")
		}
		seen[target] = true
		matches := 0
		for _, view := range snapshot.Nodes {
			if startupTarget(view.Config) == target {
				matches++
			}
		}
		if matches != 1 {
			return errors.New("exact Worker target is not configured")
		}
	}
	for _, target := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		snapshot, err = service.ConnectWorkerTarget(ctx, target, snapshot.Revision)
		if err != nil {
			return fmt.Errorf("selected Worker connection failed: %w", err)
		}
		ready := false
		for _, view := range snapshot.Nodes {
			if startupTarget(view.Config) == target {
				ready = view.Connected && view.State == "ready" && view.Issue == ""
			}
		}
		if snapshot.Issue != "" || !ready {
			return errors.New("selected Worker is not ready")
		}
	}
	return ctx.Err()
}

// setup prepares the existing product server and returns its listener/readiness
// operation. No listener or resident activation exists before every selection
// connects. Cleanup detaches adapters; it never revokes prior task authorization.
func runResidentOwner(ctx context.Context, owner residentOwner, service startupWorkers, selected []api.WorkTarget, setup func() (func() error, error)) (err error) {
	defer func() { err = errors.Join(err, owner.Close()) }()
	if err = owner.PreparePersonal(); err != nil {
		return err
	}
	if err = connectStartupWorkers(ctx, service, selected); err != nil {
		return err
	}
	publish, err := setup()
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = owner.Start(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return publish()
}
