package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Pairing contains only explicit connection coordinates and opaque product
// identity pins. The auth file is target-local; its contents never reach APP.
type ProductPairing struct {
	Mode     string `json:"mode"`
	Label    string `json:"label"`
	SSH      string `json:"ssh"`
	Helper   string `json:"helper"`
	Endpoint string `json:"endpoint"`
	AuthFile string `json:"authFile"`
	NodeID   string `json:"nodeId"`
	BotID    string `json:"botId"`
}

type ProductConnectionState struct {
	Revision        uint64         `json:"revision"`
	Pairing         ProductPairing `json:"pairing"`
	ActiveMode      string         `json:"activeMode"`
	State           string         `json:"state"`
	Issue           string         `json:"issue"`
	RestartRequired bool           `json:"restartRequired"`
}

type ProductConnectionController interface {
	ConnectionState() ProductConnectionState
	SavePairing(ProductPairing, uint64) (ProductConnectionState, error)
	Reconnect(context.Context) error
	Disconnect(context.Context) error
}

// ProductDraftPort routes a remote Bot's shared draft to its product authority.
// APP has no second draft journal or automatic send/replay queue.
type ProductDraftPort interface {
	Draft() api.Draft
	SaveDraft(api.Draft) (api.Draft, error)
}

func (s *Service) ConfigureProductConnection(controller ProductConnectionController) {
	s.mu.Lock()
	s.productConnection = controller
	s.mu.Unlock()
}

// NativeProductConnectionController is a native lifecycle port, not a Wails
// method. It lets a roaming facade delegate to a fresh local pairing authority
// without recursively traversing another Service facade.
func NativeProductConnectionController(s *Service) ProductConnectionController {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.productConnection
}

func (s *Service) ProductConnection() ProductConnectionState {
	s.mu.Lock()
	controller := s.productConnection
	s.mu.Unlock()
	if controller == nil {
		return ProductConnectionState{ActiveMode: "local", State: "local", Pairing: ProductPairing{Mode: "local"}}
	}
	return controller.ConnectionState()
}

func (s *Service) SaveProductPairing(pairing ProductPairing, revision uint64) (ProductConnectionState, error) {
	s.mu.Lock()
	controller := s.productConnection
	s.mu.Unlock()
	if controller == nil {
		return ProductConnectionState{}, errors.New("product connection setup is unavailable")
	}
	return controller.SavePairing(pairing, revision)
}

func (s *Service) ReconnectProduct(ctx context.Context) error {
	s.mu.Lock()
	controller := s.productConnection
	s.mu.Unlock()
	if controller == nil {
		return errors.New("product connection setup is unavailable")
	}
	return controller.Reconnect(ctx)
}

func (s *Service) DisconnectProduct(ctx context.Context) error {
	s.mu.Lock()
	controller := s.productConnection
	s.mu.Unlock()
	if controller == nil {
		return errors.New("product connection setup is unavailable")
	}
	return controller.Disconnect(ctx)
}
