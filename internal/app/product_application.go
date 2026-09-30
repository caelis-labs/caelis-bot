package app

import (
	"context"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
)

func newRemoteApplication(root string, host Host, pairing backend.ProductPairing) (*Application, error) {
	engine, err := newProductEngine(root, pairing, newSSHProductClient)
	if err != nil {
		return nil, err
	}
	service := backend.NewService(engine, host.ResolveFiles, host.ConsumeFiles, host.OpenURL, host.RevealFile)
	service.ConfigureInitialization(engine)
	service.ConfigureProductConnection(newProductPairingController(root, pairing, engine))
	// Surface acknowledgements and user-selected attachment metadata belong to
	// APP. They are separate from the remote Bot's draft, memory and Runtime.
	if err := service.ConfigurePresentation(filepath.Join(root, "ProductClientResources", "preview.json")); err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	return &Application{root: root, host: host, Backend: service, engine: engine, product: engine}, nil
}

func (a *Application) startRemoteProduct() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return context.Canceled
	}
	if a.started {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel, a.started = cancel, true
	a.workers.Add(2)
	go func() { defer a.workers.Done(); _ = a.product.Connect(ctx) }()
	go func() {
		defer a.workers.Done()
		observer := backend.NotificationObserver{Notify: a.host.Notify, Locale: a.host.Locale}
		var revision uint64
		for {
			snapshot, err := a.product.WaitSnapshot(ctx, revision)
			if err != nil || ctx.Err() != nil {
				return
			}
			revision = snapshot.Revision
			observer.Observe(snapshot)
			if a.host.Observe != nil {
				a.host.Observe(snapshot)
			}
		}
	}()
	return nil
}
