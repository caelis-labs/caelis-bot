package caelis

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

// CheckServiceIdle is a preflight for an explicitly confirmed shared Host
// replacement. It works with older application capabilities. Other clients can
// still submit after this read; the UI must disclose that restart affects them.
func CheckServiceIdle(ctx context.Context, settings api.RuntimeSettings) error {
	dir, err := caelisruntime.Store(settings.CaelisStore)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(dir, "runtime/service/discovery.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	d, token, err := Discover(settings)
	if err != nil {
		return err
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		return err
	}
	defer c.http.CloseIdleConnections()
	var info wire.ServerInfo
	if err = c.json(ctx, "GET", "/initialize", nil, &info, "", ""); err != nil {
		return errors.New("无法确认共享服务状态，未重启；请先检查 Caelis 服务")
	}
	if value(info.InstanceId) != d.InstanceID {
		return errors.New("共享服务已变化，请重新检测后再操作")
	}
	var status struct {
		Runtime *wire.StatusRuntime `json:"runtime"`
	}
	if err = c.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil || status.Runtime == nil {
		return errors.New("无法确认共享服务中的工作，未重启")
	}
	r := status.Runtime
	if value(r.Running) || value(r.ActiveJobs) > 0 || len(r.ActiveSessions) > 0 {
		return errors.New("Caelis 仍有工作运行或等待审批，请完成后再启用新版服务；已安装的程序会保留")
	}
	return nil
}
