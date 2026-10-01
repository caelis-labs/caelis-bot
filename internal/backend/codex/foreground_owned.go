//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"sync"
	"time"
)

// OwnedForeground delegates the native lifetime to an independent short-lived
// watchdog. Parent death, lease deadline, and an explicit stop all hard-fence
// the exact original root and retained descendants.
type OwnedForeground struct {
	process *SupervisedProcess
	mu      sync.Mutex
	epoch   string
	err     error
	cancel  context.CancelFunc
}

func StartOwnedForeground(ctx context.Context, helper, binary, directory, store string) (*OwnedForeground, error) {
	p, err := StartSupervisedProcess(ctx, SupervisedProcessOptions{HelperPath: helper, Binary: binary, Directory: directory, Store: store, Kind: SupervisedCaelisForeground})
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	o := &OwnedForeground{process: p, cancel: cancel}
	if err = p.Renew(ctx, "", 45*time.Second); err != nil {
		cancel()
		_ = p.Stop(context.Background())
		return nil, err
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-life.Done():
				return
			case <-ticker.C:
			}
			o.mu.Lock()
			if o.epoch != "" || o.err != nil {
				o.mu.Unlock()
				return
			}
			request, c := context.WithTimeout(life, 2*time.Second)
			o.err = p.Renew(request, "", 45*time.Second)
			c()
			o.mu.Unlock()
		}
	}()
	return o, nil
}
func (o *OwnedForeground) Live() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err == nil && o.process.Live()
}
func (o *OwnedForeground) ConfigureDeadline(ctx context.Context, epoch string, deadline time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil || !o.process.Live() || epoch == "" || (o.epoch != "" && o.epoch != epoch) {
		return errors.New("independent foreground watchdog unavailable")
	}
	if err := o.process.Renew(ctx, epoch, time.Until(deadline)); err != nil {
		o.err = err
		return err
	}
	o.epoch = epoch
	o.cancel()
	return nil
}
func (o *OwnedForeground) Stop(ctx context.Context) error { o.cancel(); return o.process.Stop(ctx) }
