package desktop

import (
	"errors"
	"sync"
	"testing"
)

type fakeLoginAtLogin struct {
	status LoginAtLoginStatus
	sets   []bool
	err    error
	opens  int
}

func (f *fakeLoginAtLogin) Status() LoginAtLoginStatus { return f.status }
func (f *fakeLoginAtLogin) Set(enabled bool) error {
	f.sets = append(f.sets, enabled)
	if f.err != nil {
		return f.err
	}
	if enabled {
		f.status = LoginAtLoginStatus{Supported: true, State: "on", Enabled: true, Registered: true}
	} else {
		f.status = LoginAtLoginStatus{Supported: true, State: "off"}
	}
	return nil
}
func (f *fakeLoginAtLogin) OpenSystemSettings() error { f.opens++; return f.err }

func TestLoginAtLoginUsesSystemStateAndExplicitChanges(t *testing.T) {
	s, _, _ := setup()
	f := &fakeLoginAtLogin{status: LoginAtLoginStatus{Supported: true, State: "off"}}
	s.loginAtLogin = f
	if got := s.LoginAtLoginStatus(); got.Enabled || got.Registered {
		t.Fatalf("new install should be off: %+v", got)
	}
	if len(f.sets) != 0 {
		t.Fatal("reading status registered a login item")
	}
	if got, err := s.SetLoginAtLogin(true); err != nil || !got.Enabled || len(f.sets) != 1 {
		t.Fatalf("explicit enable failed: %+v %v", got, err)
	}
	if _, err := s.SetLoginAtLogin(true); err != nil || len(f.sets) != 1 {
		t.Fatal("reopening settings registered again")
	}
	f.status = LoginAtLoginStatus{Supported: true, State: "needsApproval", Registered: true}
	if got := s.LoginAtLoginStatus(); got.Enabled || !got.Registered || got.State != "needsApproval" {
		t.Fatalf("system revocation was hidden: %+v", got)
	}
	if _, err := s.SetLoginAtLogin(true); err != nil || len(f.sets) != 1 {
		t.Fatal("system-disabled item was silently re-registered")
	}
	if err := s.OpenLoginItemsSettings(); err != nil || f.opens != 1 {
		t.Fatalf("system recovery action failed: %v", err)
	}
	if got, err := s.SetLoginAtLogin(false); err != nil || got.Registered || len(f.sets) != 2 {
		t.Fatalf("explicit removal failed: %+v %v", got, err)
	}
	if _, err := s.SetLoginAtLogin(false); err != nil || len(f.sets) != 2 {
		t.Fatal("off state was unregistered again")
	}
}

func TestLoginAtLoginFailureRetainsSystemAuthority(t *testing.T) {
	s, _, _ := setup()
	f := &fakeLoginAtLogin{status: LoginAtLoginStatus{Supported: true, State: "off"}, err: errors.New("registration denied")}
	s.loginAtLogin = f
	if got, err := s.SetLoginAtLogin(true); err == nil || got.Enabled || got.State != "off" {
		t.Fatalf("failed registration claimed success: %+v %v", got, err)
	}
	s.loginAtLogin = nil
	if got, err := s.SetLoginAtLogin(true); !errors.Is(err, errLoginAtLoginUnsupported) || got.Supported {
		t.Fatalf("unsupported host claimed a writable switch: %+v %v", got, err)
	}
}

func TestLoginAtLoginConcurrentEnableRegistersOnce(t *testing.T) {
	s, _, _ := setup()
	f := &fakeLoginAtLogin{status: LoginAtLoginStatus{Supported: true, State: "off"}}
	s.loginAtLogin = f
	var calls sync.WaitGroup
	for range 8 {
		calls.Add(1)
		go func() {
			defer calls.Done()
			_, _ = s.SetLoginAtLogin(true)
		}()
	}
	calls.Wait()
	if len(f.sets) != 1 || !s.LoginAtLoginStatus().Enabled {
		t.Fatalf("concurrent request registered %d times", len(f.sets))
	}
}
