package backend

import (
	"errors"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type approvalURLFixture struct {
	api.Engine
	calls []string
	url   string
	err   error
}

func (e *approvalURLFixture) ApprovalURL(id string) (string, error) {
	e.calls = append(e.calls, id)
	return e.url, e.err
}

type retainedURLFixture struct {
	calls   []string
	handled bool
	url     string
	err     error
}

func (e *retainedURLFixture) RetainedApprovalURL(id string) (string, bool, error) {
	e.calls = append(e.calls, id)
	return e.url, e.handled, e.err
}

func TestApprovalURLReadAndDesktopOpenUseOriginalRetainedOwner(t *testing.T) {
	resident := &approvalURLFixture{url: "http://localhost:9999/unrelated-resident"}
	owner := &retainedURLFixture{handled: true, err: errors.New("remote_native_url_not_reachable")}
	var opened []string
	s := NewService(resident, nil, nil, func(url string) error { opened = append(opened, url); return nil }, nil)
	s.ConfigureWorkerInteractions(owner)
	if _, err := s.ApprovalURL("scoped-original-request"); !errors.Is(err, owner.err) {
		t.Fatal("host URL reader lost original owner refusal", err)
	}
	if err := s.OpenApprovalURL("scoped-original-request"); !errors.Is(err, owner.err) {
		t.Fatal("desktop URL navigation lost original owner refusal", err)
	}
	if len(opened) != 0 || len(resident.calls) != 0 {
		t.Fatal("retained refusal navigated or fell through to another owner", opened, resident.calls)
	}
	owner.err = nil
	owner.url = "https://approval.example.test/original-request"
	u, err := s.ApprovalURL("scoped-original-request")
	if err != nil || u != owner.url || len(opened) != 0 {
		t.Fatal("read must return the checked original URL without navigating", u, err, opened)
	}
	if err := s.OpenApprovalURL("scoped-original-request"); err != nil || !reflect.DeepEqual(opened, []string{owner.url}) {
		t.Fatal("desktop did not navigate the same checked URL", err, opened)
	}
	for _, id := range owner.calls {
		if id != "scoped-original-request" {
			t.Fatal("URL request identity changed", owner.calls)
		}
	}
}

func TestApprovalURLFallsBackOnlyForUnownedRetainedRequest(t *testing.T) {
	resident := &approvalURLFixture{url: "https://approval.example.test/resident"}
	owner := &retainedURLFixture{handled: false}
	s := NewService(resident, nil, nil, nil, nil)
	s.ConfigureWorkerInteractions(owner)
	url, err := s.ApprovalURL("resident-original-request")
	if err != nil || url != resident.url || !reflect.DeepEqual(resident.calls, []string{"resident-original-request"}) {
		t.Fatal("resident native navigator was not preserved", url, err, resident.calls)
	}
	resident.err = errors.New("original native request resolved")
	if _, err := s.ApprovalURL("resident-original-request"); !errors.Is(err, resident.err) {
		t.Fatal("resolved native URL was exposed", err)
	}
}
