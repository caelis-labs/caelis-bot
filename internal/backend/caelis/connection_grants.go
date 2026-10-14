package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const connectionGrantCapability = "application-connection-grant-updates-v1"

type connectionGrantRequest struct {
	OperationID                                 string `json:"operation_id"`
	ExpectedAllowWorkerFullAccess               *bool  `json:"expected_allow_worker_full_access"`
	ExpectedAllowUnreviewedApplicationExecution *bool  `json:"expected_allow_unreviewed_application_execution"`
	AllowWorkerFullAccess                       *bool  `json:"allow_worker_full_access,omitempty"`
	AllowUnreviewedApplicationExecution         *bool  `json:"allow_unreviewed_application_execution,omitempty"`
}
type connectionGrantReceipt struct {
	OperationID string                     `json:"operation_id"`
	Connection  wire.ApplicationConnection `json:"connection"`
}
type connectionGrantIntent struct {
	Version       int                    `json:"version"`
	Scope         string                 `json:"scope"`
	StoreID       string                 `json:"store_id"`
	InstanceID    string                 `json:"instance_id"`
	PrincipalID   string                 `json:"principal_id"`
	ApplicationID string                 `json:"application_id"`
	ConnectionID  string                 `json:"connection_id"`
	Request       connectionGrantRequest `json:"request"`
}

func grantIntentPath(bindingPath, scope string) string {
	return filepath.Join(filepath.Dir(bindingPath), "connection-grant-"+scope+".json")
}
func grantTarget(life wire.ApplicationConnection, scope string) bool {
	if scope == "bot" {
		return value(life.AllowUnreviewedApplicationExecution)
	}
	return value(life.AllowWorkerFullAccess)
}
func grantIdentity(life, expected wire.ApplicationConnection) bool {
	return life.PrincipalId == expected.PrincipalId && life.ApplicationId == expected.ApplicationId && life.ConnectionId == expected.ConnectionId && !life.Revoked
}
func currentGrantIdentity(life, expected wire.ApplicationConnection) bool {
	return grantIdentity(life, expected) && life.ExpiresAt.After(time.Now())
}
func validGrantIntent(v connectionGrantIntent, scope, store, instance string, life wire.ApplicationConnection) bool {
	if v.Version != 1 || v.Scope != scope || v.StoreID != store || v.InstanceID != instance || v.PrincipalID != life.PrincipalId || v.ApplicationID != life.ApplicationId || v.ConnectionID != life.ConnectionId || v.Request.OperationID == "" || v.Request.ExpectedAllowWorkerFullAccess == nil || v.Request.ExpectedAllowUnreviewedApplicationExecution == nil {
		return false
	}
	if scope == "bot" {
		return v.Request.AllowWorkerFullAccess == nil && v.Request.AllowUnreviewedApplicationExecution != nil && *v.Request.AllowUnreviewedApplicationExecution
	}
	return v.Request.AllowUnreviewedApplicationExecution == nil && v.Request.AllowWorkerFullAccess != nil && *v.Request.AllowWorkerFullAccess
}

// AuthorizeFullAccess is called only after the person confirms one future
// scope in the App. The Host principal, never the Application credential,
// performs a durable CAS on this same Connection ID. Existing bindings retain
// their creation authority. Unknown writes keep the original operation.
func (s *Session) AuthorizeFullAccess(ctx context.Context, scope string) error {
	if scope != "bot" && scope != "worker" {
		return errors.New("完全访问授权作用域无效")
	}
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	life, store, instance, endpoint, current, info := s.state.Connection, s.state.StoreID, s.state.InstanceID, s.state.Endpoint, s.client, s.info
	connected, manual := s.connected && !s.closed, s.requireApproval
	settings := s.settings
	s.mu.Unlock()
	if !connected || current == nil || current.origin != endpoint || life.ConnectionId == "" || life.ApplicationId == "" || life.PrincipalId == "" {
		return errors.New("请先连接 Caelis 并核对当前 Host 授权")
	}
	if scope == "bot" && manual {
		return errors.New("当前 Caelis 会话固定人工批准，无法授权新对话完全访问")
	}
	creationCapability := "worker-creation-permissions-v1"
	if scope == "bot" {
		creationCapability = "application-execution-approval-never-v1"
	}
	if !current.schema || !slices.Contains(info.Capabilities, creationCapability) {
		return errors.New("当前 Caelis Host 不支持创建时固定完全访问权限；请更新并重启 Host")
	}
	d, token, err := Discover(settings)
	if err != nil {
		return err
	}
	if d.PrincipalID != life.PrincipalId || d.InstanceID != instance || d.Endpoint != endpoint {
		return errors.New("Caelis Host 身份已改变；请先重新连接，原授权不变")
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		return err
	}
	defer host.http.CloseIdleConnections()
	observed, err := initialize(ctx, host)
	if err != nil {
		return err
	}
	if value(observed.StoreId) != store || value(observed.InstanceId) != instance || !host.schema || !slices.Contains(observed.Capabilities, creationCapability) {
		return errors.New("Caelis Host 身份或授权合同已改变；请更新并重新连接")
	}
	intentPath := grantIntentPath(s.path, scope)
	var intent connectionGrantIntent
	if raw, readErr := privateRead(intentPath, 65536); readErr == nil {
		if json.Unmarshal(raw, &intent) != nil || !validGrantIntent(intent, scope, store, instance, life) {
			return errors.New("Caelis 原授权请求无法核对；已保留记录，请恢复原 Host 或联系支持")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	// A previously Host-enrolled grant remains usable on a compatible older
	// Host. Only increasing an absent grant requires the new update endpoint.
	if !slices.Contains(info.Capabilities, connectionGrantCapability) || !slices.Contains(observed.Capabilities, connectionGrantCapability) {
		if intent.Version != 0 {
			return errors.New("原 Host 授权请求尚未核对；请恢复支持授权回执的 Host 后重试")
		}
		var scoped wire.ApplicationConnection
		if err := current.json(ctx, "GET", "/application/connection", nil, &scoped, "", ""); err != nil {
			return err
		}
		if !currentGrantIdentity(scoped, life) || !grantTarget(scoped, scope) {
			return errors.New("当前 Caelis Host 不支持原连接授权变更；请更新并重启 Host")
		}
		s.mu.Lock()
		if !s.connected || s.closed || s.state.Connection.ConnectionId != life.ConnectionId || s.state.InstanceID != instance {
			s.mu.Unlock()
			return errors.New("Caelis 连接在授权期间改变；请重新连接并核对")
		}
		s.state.Connection = scoped
		s.bumpLocked()
		s.mu.Unlock()
		return nil
	}
	path := "/applications/connections/" + idPath(life.ConnectionId)
	var hostLife wire.ApplicationConnection
	if err := host.json(ctx, "GET", path, nil, &hostLife, "", ""); err != nil {
		return err
	}
	if !currentGrantIdentity(hostLife, life) {
		return errors.New("Caelis Host 回读的连接身份不匹配；原授权保持不变")
	}
	if intent.Version == 0 && !grantTarget(hostLife, scope) {
		trueValue := true
		intent = connectionGrantIntent{Version: 1, Scope: scope, StoreID: store, InstanceID: instance, PrincipalID: life.PrincipalId, ApplicationID: life.ApplicationId, ConnectionID: life.ConnectionId,
			Request: connectionGrantRequest{OperationID: "grant-" + rand.Text(), ExpectedAllowWorkerFullAccess: pointer(value(hostLife.AllowWorkerFullAccess)), ExpectedAllowUnreviewedApplicationExecution: pointer(value(hostLife.AllowUnreviewedApplicationExecution))}}
		if scope == "bot" {
			intent.Request.AllowUnreviewedApplicationExecution = &trueValue
		} else {
			intent.Request.AllowWorkerFullAccess = &trueValue
		}
		if err := privateWrite(intentPath, intent); err != nil {
			return err
		}
	}
	if intent.Version != 0 {
		var receipt connectionGrantReceipt
		receiptPath := path + "/grant-updates/" + idPath(intent.Request.OperationID)
		readErr := host.json(ctx, "GET", receiptPath, nil, &receipt, "", "")
		if isRemoteStatus(readErr, 404) {
			var posted connectionGrantReceipt
			postErr := host.json(ctx, "POST", path+"/grants", intent.Request, &posted, intent.Request.OperationID, "")
			// The original operation is the only permitted retry. A lost POST
			// response remains unknown until its immutable receipt is observed.
			readErr = host.json(ctx, "GET", receiptPath, nil, &receipt, "", "")
			if readErr != nil {
				if isRemoteStatus(postErr, 401) || isRemoteStatus(postErr, 403) {
					return errors.New("Caelis Host 未接受本机主体的授权变更；原请求已保留，请核对 Host 授权后重试")
				}
				if isRemoteStatus(postErr, 409) && isRemoteStatus(readErr, 404) {
					return errors.New("Caelis Host 授权状态与原请求冲突；原操作已保留，请停止其他授权变更并联系支持核对")
				}
				return fmt.Errorf("Caelis Host 授权结果尚未确认；原请求已保留，请重试核对: %w", errors.Join(postErr, readErr))
			}
		}
		if readErr != nil {
			return readErr
		}
		if receipt.OperationID != intent.Request.OperationID || !grantIdentity(receipt.Connection, life) || !grantTarget(receipt.Connection, scope) ||
			(scope == "bot" && value(receipt.Connection.AllowWorkerFullAccess) != *intent.Request.ExpectedAllowWorkerFullAccess) ||
			(scope == "worker" && value(receipt.Connection.AllowUnreviewedApplicationExecution) != *intent.Request.ExpectedAllowUnreviewedApplicationExecution) {
			return errors.New("Caelis Host 授权回执与原请求不一致；原请求已保留")
		}
	}
	var verifiedHost, verifiedApp wire.ApplicationConnection
	if err := host.json(ctx, "GET", path, nil, &verifiedHost, "", ""); err != nil {
		return err
	}
	if err := current.json(ctx, "GET", "/application/connection", nil, &verifiedApp, "", ""); err != nil {
		return err
	}
	if !currentGrantIdentity(verifiedHost, life) || !currentGrantIdentity(verifiedApp, life) || !grantTarget(verifiedHost, scope) || !grantTarget(verifiedApp, scope) || value(verifiedHost.AllowWorkerFullAccess) != value(verifiedApp.AllowWorkerFullAccess) || value(verifiedHost.AllowUnreviewedApplicationExecution) != value(verifiedApp.AllowUnreviewedApplicationExecution) {
		return errors.New("Caelis Host 与应用授权回读不一致；未来模式未保存，请刷新核对")
	}
	s.mu.Lock()
	if s.connected && !s.closed && s.state.Connection.ConnectionId == life.ConnectionId && s.state.InstanceID == instance {
		s.state.Connection = verifiedApp
		s.bumpLocked()
	} else {
		s.mu.Unlock()
		return errors.New("Caelis 连接在授权期间改变；请重新连接并核对")
	}
	s.mu.Unlock()
	if intent.Version != 0 {
		if err := os.Remove(intentPath); err != nil {
			return errors.New("Host 已授权，但原请求记录未能清理；请重试核对")
		}
	}
	return nil
}
