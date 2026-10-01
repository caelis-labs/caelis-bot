//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type roamingWorkerOriginBook struct {
	mu         sync.Mutex
	file       string
	Pair       workerwire.Pair   `json:"pair"`
	Schema     int               `json:"schema"`
	Generation string            `json:"generation"`
	Epoch      string            `json:"epoch"`
	State      string            `json:"state"`
	Requests   map[string]string `json:"requests"`
	Tasks      map[string]string `json:"tasks"`
}

func openRoamingWorkerOrigins(directory string, pair workerwire.Pair) (*roamingWorkerOriginBook, error) {
	book := &roamingWorkerOriginBook{file: filepath.Join(directory, "origins.json"), Pair: pair, Schema: 1, Requests: map[string]string{}, Tasks: map[string]string{}}
	data, err := readBrokerFile(book.file, 2<<20)
	// readBrokerFile deliberately masks missing/invalid files, so check absence
	// separately; an existing unreadable original ledger never grants replacement.
	if err != nil {
		if _, statErr := os.Lstat(book.file); errors.Is(statErr, os.ErrNotExist) {
			entries, readErr := os.ReadDir(directory)
			if readErr != nil {
				return nil, readErr
			}
			for _, entry := range entries {
				if entry.Name() != "worker-pair.json" {
					return nil, errors.New("previous Worker owner has no retained generation ledger")
				}
			}
			return book, nil
		}
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(book) != nil || decoder.Decode(&struct{}{}) != io.EOF || book.Schema != 1 || book.Pair != pair || book.Requests == nil || book.Tasks == nil || len(book.Requests) > 4096 || len(book.Tasks) > 4096 {
		return nil, errors.New("original Worker generation ledger unavailable")
	}
	if len(book.Generation) > 128 || len(book.Epoch) > 256 || (book.State != "active" && book.State != "preparing" && book.State != "stopped") {
		return nil, errors.New("original Worker generation identity unavailable")
	}
	for _, origins := range []map[string]string{book.Tasks, book.Requests} {
		for id, generation := range origins {
			if id == "" || len(id) > 128 || generation == "" || len(generation) > 128 {
				return nil, errors.New("original Worker operation identity unavailable")
			}
		}
	}
	return book, nil
}
func (b *roamingWorkerOriginBook) writeLocked() error { return localstate.Write(b.file, b) }
func (b *roamingWorkerOriginBook) begin(generation, epoch string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Generation != "" && b.State != "stopped" {
		return errors.New("previous owned Worker stop remains unconfirmed")
	}
	b.Generation, b.Epoch, b.State = generation, epoch, "preparing"
	return b.writeLocked()
}
func (b *roamingWorkerOriginBook) state(generation, state string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Generation != generation {
		return errors.New("Worker generation changed")
	}
	b.State = state
	return b.writeLocked()
}
func (b *roamingWorkerOriginBook) epoch() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Epoch }
func (b *roamingWorkerOriginBook) currentTask(generation, id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Tasks[id] == "" || b.Tasks[id] == generation
}
func (b *roamingWorkerOriginBook) currentRequest(generation, id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Requests[id] == "" || b.Requests[id] == generation
}

type roamingWorkerGenerationGuard struct {
	book       *roamingWorkerOriginBook
	generation string
	native     nodeworker.Client
}

func (g *roamingWorkerGenerationGuard) admit(ctx context.Context) error {
	if err := g.native.WorkAdmission(ctx); err != nil {
		return err
	}
	source, err := workerwire.SourceProvider().WorkDispatchSource(ctx)
	if err != nil {
		return err
	}
	g.book.mu.Lock()
	defer g.book.mu.Unlock()
	if source.Lease.Epoch == "" || g.book.Generation != g.generation || g.book.State != "active" || (g.book.Epoch != "" && g.book.Epoch != source.Lease.Epoch) {
		return errors.New("Worker generation source epoch changed")
	}
	if g.book.Epoch == "" {
		g.book.Epoch = source.Lease.Epoch
		return g.book.writeLocked()
	}
	return nil
}
func (g *roamingWorkerGenerationGuard) record(ctx context.Context, request, task string) error {
	if task == "" || len(task) > 128 || len(request) > 128 {
		return errors.New("invalid bounded original Worker identity")
	}
	if request != "" {
		g.book.mu.Lock()
		known := g.book.Requests[request] == g.generation && g.book.Tasks[task] == g.generation
		g.book.mu.Unlock()
		// Existing original IDs go only to this original native adapter's receipt
		// reconciliation; they never receive new admission or another generation.
		if known {
			return nil
		}
	}

	if !g.book.currentTask(g.generation, task) || request != "" && !g.book.currentRequest(g.generation, request) {
		return errors.New("original Worker operation belongs to a retained previous generation")
	}
	if err := g.admit(ctx); err != nil {
		return err
	}
	g.book.mu.Lock()
	defer g.book.mu.Unlock()
	if len(g.book.Requests) >= 4096 || len(g.book.Tasks) >= 4096 {
		return errors.New("Worker original operation ledger limit")
	}
	if old := g.book.Tasks[task]; old != "" && old != g.generation {
		return errors.New("original task generation differs")
	}
	if request != "" {
		if old := g.book.Requests[request]; old != "" && old != g.generation {
			return errors.New("original request generation differs")
		}
		g.book.Requests[request] = g.generation
	}
	g.book.Tasks[task] = g.generation
	return g.book.writeLocked()
}
func (g *roamingWorkerGenerationGuard) unknown(id string) (api.Task, error) {
	target := g.book.Pair.Target
	return api.Task{ID: id, Target: &target, Status: "unknown", Outcome: "unknown"}, errors.New("original Worker outcome remains in its retained generation")
}

type roamingCodexGenerationWorker struct {
	*codex.WorkerClient
	guard *roamingWorkerGenerationGuard
}

func (w *roamingCodexGenerationWorker) WorkAdmission(ctx context.Context) error {
	return w.guard.admit(ctx)
}
func (w *roamingCodexGenerationWorker) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, in.ID) || !w.guard.book.currentRequest(w.guard.generation, in.RequestID) {
		return w.guard.unknown(in.ID)
	}
	if err := w.guard.record(ctx, in.RequestID, in.ID); err != nil {
		return api.Task{}, err
	}
	return w.WorkerClient.StartWork(ctx, in)
}
func (w *roamingCodexGenerationWorker) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, in.ID) || !w.guard.book.currentRequest(w.guard.generation, in.RequestID) {
		return w.guard.unknown(in.ID)
	}
	if err := w.guard.record(ctx, in.RequestID, in.ID); err != nil {
		return api.Task{}, err
	}
	return w.WorkerClient.SendWork(ctx, in)
}
func (w *roamingCodexGenerationWorker) ReadWork(ctx context.Context, id string) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, id) {
		return w.guard.unknown(id)
	}
	return w.WorkerClient.ReadWork(ctx, id)
}
func (w *roamingCodexGenerationWorker) StopWork(ctx context.Context, id string) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, id) {
		return w.guard.unknown(id)
	}
	return w.WorkerClient.StopWork(ctx, id)
}
func (w *roamingCodexGenerationWorker) PrepareWorkWorkspace(ctx context.Context, id, path string, selected bool) error {
	if err := w.guard.record(ctx, "", id); err != nil {
		return err
	}
	return w.WorkerClient.PrepareWorkWorkspace(ctx, id, path, selected)
}
func (w *roamingCodexGenerationWorker) DecideWork(ctx context.Context, approval api.WorkApproval, decision api.Decision) error {
	if !w.guard.book.currentTask(w.guard.generation, approval.TaskID) {
		return errors.New("approval belongs to retained original Worker generation")
	}
	return w.WorkerClient.DecideWork(ctx, approval, decision)
}
func (w *roamingCodexGenerationWorker) WorkMessageRecorded(in api.TaskMessage) bool {
	return w.guard.book.currentTask(w.guard.generation, in.ID) && w.guard.book.currentRequest(w.guard.generation, in.RequestID) && w.WorkerClient.WorkMessageRecorded(in)
}

func (w *roamingPairedCaelisWorker) WorkAdmission(ctx context.Context) error {
	return w.guard.admit(ctx)
}
func (w *roamingPairedCaelisWorker) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, in.ID) || !w.guard.book.currentRequest(w.guard.generation, in.RequestID) {
		return w.guard.unknown(in.ID)
	}
	if err := w.guard.record(ctx, in.RequestID, in.ID); err != nil {
		return api.Task{}, err
	}
	return w.LeasedWorker.StartWork(ctx, in)
}
func (w *roamingPairedCaelisWorker) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, in.ID) || !w.guard.book.currentRequest(w.guard.generation, in.RequestID) {
		return w.guard.unknown(in.ID)
	}
	if err := w.guard.record(ctx, in.RequestID, in.ID); err != nil {
		return api.Task{}, err
	}
	return w.LeasedWorker.SendWork(ctx, in)
}
func (w *roamingPairedCaelisWorker) ReadWork(ctx context.Context, id string) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, id) {
		return w.guard.unknown(id)
	}
	return w.LeasedWorker.ReadWork(ctx, id)
}
func (w *roamingPairedCaelisWorker) StopWork(ctx context.Context, id string) (api.Task, error) {
	if !w.guard.book.currentTask(w.guard.generation, id) {
		return w.guard.unknown(id)
	}
	return w.LeasedWorker.StopWork(ctx, id)
}
func (w *roamingPairedCaelisWorker) PrepareWorkWorkspace(ctx context.Context, id, path string, selected bool) error {
	if err := w.guard.record(ctx, "", id); err != nil {
		return err
	}
	return w.LeasedWorker.PrepareWorkWorkspace(ctx, id, path, selected)
}
func (w *roamingPairedCaelisWorker) DecideWork(ctx context.Context, approval api.WorkApproval, decision api.Decision) error {
	if !w.guard.book.currentTask(w.guard.generation, approval.TaskID) {
		return errors.New("approval belongs to retained original Worker generation")
	}
	return w.LeasedWorker.DecideWork(ctx, approval, decision)
}
func (w *roamingPairedCaelisWorker) WorkMessageRecorded(in api.TaskMessage) bool {
	return w.guard.book.currentTask(w.guard.generation, in.ID) && w.guard.book.currentRequest(w.guard.generation, in.RequestID) && w.LeasedWorker.WorkMessageRecorded(in)
}
