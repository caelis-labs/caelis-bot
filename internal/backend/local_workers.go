package backend

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) ConfigureLocalWorkers(v api.LocalWorkerController) { s.localWorkers = v }
func (s *Service) InspectLocalWorker(runtime string) (api.LocalWorkerSettings, error) {
	if s.localWorkers == nil {
		return api.LocalWorkerSettings{}, errors.New("workers unavailable")
	}
	ctx, cancel := s.machineContext()
	defer cancel()
	return s.localWorkers.InspectLocalWorker(ctx, runtime)
}
func (s *Service) SaveLocalWorkerModel(v api.WorkExecutionSettings) (api.LocalWorkerSettings, error) {
	if s.localWorkers == nil {
		return api.LocalWorkerSettings{}, errors.New("workers unavailable")
	}
	ctx, cancel := s.machineContext()
	defer cancel()
	out, err := s.localWorkers.SaveLocalWorkerModel(ctx, v)
	if err == nil {
		if p, ok := s.engine.(api.Provider); ok && p.ProviderInfo().ID == out.Runtime {
			s.configurationMu.Lock()
			s.workExecutionSettings = out.Work
			s.configurationMu.Unlock()
		}
	}
	return out, err
}
