package caelis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

type Options struct {
	Directory string
	Settings  api.RuntimeSettings
	Execution api.ExecutionSettings
	// ToolsOnly is for protocol acceptance without native execution. Product
	// assembly always requests workspace-write; it never silently falls back.
	ToolsOnly bool
}
type Session struct {
	mu                sync.Mutex
	step              sync.Mutex
	path              string
	settings          api.RuntimeSettings
	execution         api.ExecutionSettings
	executionMode     string
	tools             *api.ToolConnection
	catalog           map[string]api.ApplicationTools
	catalogs          map[string]map[string]api.ApplicationTools
	profile           wire.ApplicationProfile
	state             binding
	loadErr           error
	client            *client
	info              wire.ServerInfo
	connected, closed bool
	issue             string
	revision          uint64
	changed           chan struct{}
	cancel            context.CancelFunc
	ctx               context.Context
	wg                sync.WaitGroup
	streamCtx         context.Context
	streamCancel      context.CancelFunc
	generation        uint64
	wake              chan struct{}
	streams           map[string]bool
}

func New(opts Options) *Session {
	p := filepath.Join(opts.Directory, "application.json")
	b, e := loadBinding(p)
	mode := "workspace-write"
	if opts.ToolsOnly {
		mode = "tools-only"
	}
	return &Session{path: p, settings: opts.Settings, execution: opts.Execution, executionMode: mode, state: b, loadErr: e, revision: 1, changed: make(chan struct{}), streams: map[string]bool{}, wake: make(chan struct{}, 1)}
}
func (*Session) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "caelis", Name: "Caelis", ConnectionKind: "local-host", HelpURL: "https://caelis.dev", ConnectionHint: "使用本机 Caelis；安装与模型凭据由运行时管理。"}
}
func (s *Session) Connect(ctx context.Context) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("连接已停止")
	}
	if s.connected {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if s.loadErr != nil {
		return s.fail(s.loadErr)
	}
	if s.tools == nil {
		return s.fail(errors.New("应用工具尚未配置"))
	}
	if e := s.connect(ctx); e != nil {
		return s.fail(e)
	}
	s.mu.Lock()
	if s.cancel == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.wg.Add(2)
		go s.pollLoop(s.ctx)
		go s.callLoop(s.ctx)
	}
	s.connected = true
	s.issue = ""
	s.bumpLocked()
	s.mu.Unlock()
	return nil
}
func (s *Session) fail(e error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = false
	s.issue = e.Error()
	s.bumpLocked()
	return e
}

type discovery struct {
	Schema      string `json:"schema_version"`
	Endpoint    string `json:"endpoint"`
	InstanceID  string `json:"instance_id"`
	PrincipalID string `json:"principal_id"`
}

func Discover(settings api.RuntimeSettings) (discovery, string, error) {
	dir, e := caelisruntime.Store(settings.CaelisStore)
	if e != nil {
		return discovery{}, "", e
	}
	raw, e := privateRead(filepath.Join(dir, "runtime/service/discovery.json"), 65536)
	if e != nil {
		return discovery{}, "", errors.New("Caelis 服务尚未就绪，请在连接设置中启动服务")
	}
	var d discovery
	if json.Unmarshal(raw, &d) != nil || d.Schema != "caelis.control.service-discovery/v1" || d.PrincipalID == "" {
		return d, "", errors.New("Caelis 服务发现记录不兼容")
	}
	b, e := privateRead(filepath.Join(dir, "runtime/service/auth.token"), 65536)
	if e != nil {
		return d, "", errors.New("无法读取本机 Caelis 的受保护凭据")
	}
	return d, strings.TrimSpace(string(b)), nil
}

var required = []string{"application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1"}
var errBotIncompatible = errors.New("Caelis 应用协议不兼容，请更新运行时并重启 Caelis 服务")

func initialize(ctx context.Context, c *client) (wire.ServerInfo, error) {
	var i wire.ServerInfo
	e := c.json(ctx, "GET", "/initialize", nil, &i, "", "")
	if e != nil {
		return i, e
	}
	if i.ProtocolVersion != 1 || i.ApiVersion != "v1" || i.EnvelopeVersion != "caelis.control.envelope/v1" || value(i.StoreId) == "" || value(i.InstanceId) == "" {
		return i, errBotIncompatible
	}
	for _, cap := range required {
		if !slices.Contains(i.Capabilities, cap) {
			return i, errBotIncompatible
		}
	}
	return i, nil
}
func Probe(ctx context.Context, settings api.RuntimeSettings) error {
	return ProbeBinding(ctx, settings, "")
}

// ProbeBinding validates the existing identity without registering or activating.
func ProbeBinding(ctx context.Context, settings api.RuntimeSettings, directory string) error {
	if _, e := caelisruntime.Find(settings.CLIPath); e != nil {
		return e
	}
	d, t, e := Discover(settings)
	if e != nil {
		return e
	}
	c, e := newClient(d.Endpoint, t)
	if e != nil {
		return e
	}
	i, e := initialize(ctx, c)
	if e == nil && value(i.InstanceId) != d.InstanceID {
		return errors.New("Caelis 服务已替换，请重新检测")
	}
	if e != nil || directory == "" {
		return e
	}
	b, e := loadBinding(filepath.Join(directory, "application.json"))
	if e != nil {
		return e
	}
	if b.StoreID != "" && (b.StoreID != value(i.StoreId) || b.PrincipalID != d.PrincipalID) {
		return errors.New("所选 Caelis 数据目录与已有 Bot 身份不一致，原有连接保持不变")
	}
	return nil
}

// Enrollment is the only Host-authorized mutation. Credential and operation
// identity are durable BEFORE dispatch; every later request uses app scope.
type credential struct {
	StoreID, PrincipalID, OperationID, Token string
}

func (s *Session) connect(ctx context.Context) error {
	d, token, e := Discover(s.settings)
	if e != nil {
		return e
	}
	host, e := newClient(d.Endpoint, token)
	if e != nil {
		return e
	}
	defer host.http.CloseIdleConnections()
	info, e := initialize(ctx, host)
	if e != nil {
		return e
	}
	if value(info.InstanceId) != d.InstanceID {
		return errors.New("Caelis 服务发现记录已过期")
	}
	s.mu.Lock()
	mismatch := s.state.StoreID != "" && (s.state.StoreID != value(info.StoreId) || s.state.PrincipalID != d.PrincipalID)
	s.mu.Unlock()
	if mismatch {
		return errors.New("Caelis 数据目录或授权主体已改变，原有绑定保持不变")
	}
	var key credential
	raw, e := privateRead(secretPath(s.path), 65536)
	fresh := errors.Is(e, os.ErrNotExist)
	if fresh {
		secret := make([]byte, 32)
		if _, e = rand.Read(secret); e != nil {
			return e
		}
		key = credential{StoreID: value(info.StoreId), PrincipalID: d.PrincipalID, OperationID: "register-" + rand.Text(), Token: "app-client-" + hex.EncodeToString(secret)}
		if e = privateWrite(secretPath(s.path), key); e != nil {
			return e
		}
	} else if e != nil {
		return e
	} else if json.Unmarshal(raw, &key) != nil || key.StoreID != value(info.StoreId) || key.PrincipalID != d.PrincipalID || key.Token == "" || key.OperationID == "" {
		return errors.New("Caelis 应用凭据与绑定不一致")
	}
	scoped, e := newClient(d.Endpoint, key.Token)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		if !ok {
			scoped.http.CloseIdleConnections()
		}
	}()
	var life wire.ApplicationConnection
	if !fresh {
		e = scoped.json(ctx, "GET", "/application/connection", nil, &life, "", "")
	}
	if fresh || isRemoteStatus(e, 401) {
		// Replaying the exact registration is safe even after a lost response. This
		// never creates a new credential on expiry/revocation or changes app scope.
		e = host.json(ctx, "POST", "/applications/register", wire.ApplicationRegistration{OperationId: key.OperationID, Name: "Caelis Bot", Credential: key.Token}, &life, key.OperationID, "")
	}
	if e != nil {
		return e
	}
	if life.PrincipalId != d.PrincipalID || life.ApplicationId == "" || life.ConnectionId == "" || life.Revoked {
		return errors.New("Caelis 应用授权无效或已撤销")
	}
	s.mu.Lock()
	previous := s.state.Connection
	s.mu.Unlock()
	if previous.ConnectionId != "" && (previous.ConnectionId != life.ConnectionId || previous.ApplicationId != life.ApplicationId) {
		return errors.New("Caelis 应用授权标识改变")
	}
	if !life.ExpiresAt.After(time.Now().Add(3 * time.Minute)) {
		if e = scoped.json(ctx, "POST", "/application/connection/renew", struct{}{}, &life, "", ""); e != nil {
			return e
		}
	}
	s.mu.Lock()
	old := s.client
	s.client = scoped
	s.info = info
	s.state.StoreID = value(info.StoreId)
	s.state.PrincipalID = d.PrincipalID
	s.state.Endpoint = d.Endpoint
	s.state.Connection = life
	s.generation++
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.streamCtx = nil
	s.streamCancel = nil
	s.streams = map[string]bool{}
	s.state.InstanceID = value(info.InstanceId)
	e = s.saveLocked()
	s.mu.Unlock()
	if old != nil {
		old.http.CloseIdleConnections()
	}
	if e != nil {
		return e
	}
	if e = s.ensureSession(ctx, host); e != nil {
		return e
	}
	ok = true
	return nil
}
func isRemoteStatus(e error, status int) bool {
	var r *remoteError
	return errors.As(e, &r) && r.Status == status
}

// Detaching observation does not revoke a lease, cancel a turn or stop the Host.
func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.connected = false
	if s.cancel != nil {
		s.cancel()
	}
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.bumpLocked()
	s.mu.Unlock()
	s.wg.Wait()
	s.step.Lock()
	defer s.step.Unlock()
	if s.client != nil {
		s.client.http.CloseIdleConnections()
	}
	return nil
}
