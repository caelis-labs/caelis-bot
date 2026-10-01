package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// ManagedStartConfig is an explicitly installed, target-private native plan.
// It cannot be supplied by an incoming Start request. Every executable and SSH
// pairing is bound before the original source is retired.
type ManagedStartConfig struct {
	WorkersFile      string                 `json:"workersFile,omitempty"`
	Version          int                    `json:"version"`
	NodeID           string                 `json:"nodeId"`
	Directory        string                 `json:"directory"`
	Helper           string                 `json:"helper"`
	HelperSHA256     string                 `json:"helperSha256"`
	RuntimeDirectory string                 `json:"runtimeDirectory,omitempty"`
	CodexBinary      string                 `json:"codexBinary,omitempty"`
	Bindings         []ManagedBrokerBinding `json:"bindings"`
}
type ManagedBrokerBinding struct {
	BotID         string    `json:"botId"`
	BrokerNodeID  string    `json:"brokerNodeId"`
	SSH           SSHConfig `json:"ssh"`
	Helper        string    `json:"helper,omitempty"`
	Socket        string    `json:"socket"`
	JoinDirectory string    `json:"joinDirectory,omitempty"`
	AuthFile      string    `json:"authFile"`
}
type ManagedStartRequest struct {
	NodeID       string `json:"nodeId"`
	OperationID  string `json:"operationId"`
	BotID        string `json:"botId"`
	BrokerNodeID string `json:"brokerNodeId"`
}
type ManagedStartReceipt struct {
	OperationID  string `json:"operationId"`
	Outcome      string `json:"outcome"`
	NodeID       string `json:"nodeId"`
	BotID        string `json:"botId"`
	BrokerNodeID string `json:"brokerNodeId"`
}
type ManagedStartBinding struct {
	BotID        string `json:"botId"`
	BrokerNodeID string `json:"brokerNodeId"`
}
type ManagedStartStatus struct {
	NodeID   string                `json:"nodeId"`
	Bindings []ManagedStartBinding `json:"bindings"`
}
type ManagedStartPort interface {
	ManagedRoamingStatus(context.Context, string) (ManagedStartStatus, error)
	StartManagedRoaming(context.Context, ManagedStartRequest) (ManagedStartReceipt, error)
}
type startRecord struct {
	request ManagedStartRequest
	receipt ManagedStartReceipt
}

// ManagedStarter owns one foreground managed helper lifetime, independently of
// observing APP streams. It starts no service and installs no network policy.
type ManagedStarter struct {
	mu          sync.Mutex
	life        context.Context
	config      ManagedStartConfig
	records     map[string]startRecord
	command     *exec.Cmd
	childSocket string
	childDone   chan struct{}
	activeReady chan struct{}
}

func verifyManagedHelper(c ManagedStartConfig) error {
	if !filepath.IsAbs(c.Helper) || filepath.Clean(c.Helper) != c.Helper || len(c.HelperSHA256) != 64 {
		return errors.New("verified native host helper required")
	}
	info, err := os.Lstat(c.Helper)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > 256<<20 {
		return errors.New("native host helper unavailable")
	}
	f, err := os.Open(c.Helper)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("native helper changed")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, 256<<20+1)); err != nil || hex.EncodeToString(h.Sum(nil)) != c.HelperSHA256 {
		return errors.New("native helper checksum mismatch")
	}
	return nil
}
func LoadManagedStarter(ctx context.Context, directory, nodeID, path string) (*ManagedStarter, error) {
	var config ManagedStartConfig
	if err := readPrivateJSON(path, &config); err != nil {
		return nil, err
	}
	if config.Directory != directory || config.NodeID != nodeID {
		return nil, errors.New("managed native enrollment mismatch")
	}
	return NewManagedStarter(ctx, config)
}
func NewManagedStarter(ctx context.Context, c ManagedStartConfig) (*ManagedStarter, error) {
	if c.Version != 1 || !identifier.MatchString(c.NodeID) || len(c.Bindings) == 0 || len(c.Bindings) > 32 || CheckPrivateDirectory(c.Directory) != nil || !validSocket(filepath.Join(c.Directory, "managed.sock")) {
		return nil, errors.New("invalid private managed native plan")
	}
	var enrollment struct {
		ID string `json:"id"`
	}
	if err := readPrivateJSON(filepath.Join(c.Directory, "node.json"), &enrollment); err != nil || enrollment.ID != c.NodeID {
		return nil, errors.New("managed existing enrollment mismatch")
	}
	if err := verifyManagedHelper(c); err != nil {
		return nil, err
	}
	if c.CodexBinary != "" && !filepath.IsAbs(c.CodexBinary) || c.RuntimeDirectory != "" && !filepath.IsAbs(c.RuntimeDirectory) {
		return nil, errors.New("target-local Runtime paths required")
	}
	seen := map[ManagedStartBinding]bool{}
	for _, b := range c.Bindings {
		key := ManagedStartBinding{b.BotID, b.BrokerNodeID}
		if !identifier.MatchString(b.BotID) || !identifier.MatchString(b.BrokerNodeID) || seen[key] || !validSocket(b.Socket) || !filepath.IsAbs(b.AuthFile) || filepath.Clean(b.AuthFile) != b.AuthFile {
			return nil, errors.New("invalid exact managed broker binding")
		}
		if b.SSH.Target != "" {
			if _, err := JoinArgs(b.SSH, b.Helper, b.JoinDirectory, filepath.Join(c.Directory, "managed.sock")); err != nil {
				return nil, err
			}
		}
		if b.SSH.Target == "" && (b.Helper != "" || b.JoinDirectory != "") {
			return nil, errors.New("partial managed outgoing broker binding")
		}
		seen[key] = true
	}
	m := &ManagedStarter{life: ctx, config: c, records: map[string]startRecord{}}
	context.AfterFunc(ctx, func() { _ = m.Close() })
	return m, nil
}
func (m *ManagedStarter) ManagedRoamingStatus(ctx context.Context, nodeID string) (ManagedStartStatus, error) {
	if nodeID != m.config.NodeID || ctx.Err() != nil || m.life.Err() != nil {
		return ManagedStartStatus{}, errors.New("managed native owner unavailable")
	}
	if err := verifyManagedHelper(m.config); err != nil {
		return ManagedStartStatus{}, err
	}
	result := ManagedStartStatus{NodeID: nodeID, Bindings: []ManagedStartBinding{}}
	for _, b := range m.config.Bindings {
		result.Bindings = append(result.Bindings, ManagedStartBinding{b.BotID, b.BrokerNodeID})
	}
	return result, nil
}
func (m *ManagedStarter) StartManagedRoaming(ctx context.Context, r ManagedStartRequest) (ManagedStartReceipt, error) {
	m.mu.Lock()
	if r.NodeID != m.config.NodeID || !identifier.MatchString(r.OperationID) || m.life.Err() != nil {
		m.mu.Unlock()
		return ManagedStartReceipt{}, errors.New("managed native scope mismatch")
	}
	if old, ok := m.records[r.OperationID]; ok {
		m.mu.Unlock()
		if old.request != r {
			return ManagedStartReceipt{}, errors.New("managed operation identity reused")
		}
		return old.receipt, nil
	}
	rejected := ManagedStartReceipt{OperationID: r.OperationID, Outcome: "rejected", NodeID: r.NodeID, BotID: r.BotID, BrokerNodeID: r.BrokerNodeID}
	var binding *ManagedBrokerBinding
	for i := range m.config.Bindings {
		b := &m.config.Bindings[i]
		if b.BotID == r.BotID && b.BrokerNodeID == r.BrokerNodeID {
			binding = b
			break
		}
	}
	if binding == nil || m.command != nil || len(m.records) >= 128 {
		m.mu.Unlock()
		return rejected, nil
	}
	if err := verifyManagedHelper(m.config); err != nil {
		m.mu.Unlock()
		return rejected, err
	}
	sum := sha256.Sum256([]byte(r.BotID))
	generations := filepath.Join(m.config.Directory, "roaming-"+hex.EncodeToString(sum[:6]))
	args := []string{"serve-roaming", "--managed-agent", "--node-id", r.NodeID, "--bot-id", r.BotID, "--agent-directory", m.config.Directory, "--generations", generations, "--broker-node-id", r.BrokerNodeID, "--broker-socket", binding.Socket, "--auth-file", binding.AuthFile}
	if m.config.WorkersFile != "" {
		args = append(args, "--workers-file", m.config.WorkersFile)
	}
	if m.config.CodexBinary != "" {
		args = append(args, "--codex-binary", m.config.CodexBinary)
	}
	if m.config.RuntimeDirectory != "" {
		args = append(args, "--runtime-directory", m.config.RuntimeDirectory)
	}
	if binding.SSH.Target != "" {
		args = append(args, "--broker-ssh-target", binding.SSH.Target, "--broker-helper", binding.Helper, "--join-target", binding.SSH.Target, "--join-helper", binding.Helper, "--join-directory", binding.JoinDirectory)
	}
	cmd := exec.Command(m.config.Helper, args...)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.mu.Unlock()
		return rejected, err
	}
	if err = cmd.Start(); err != nil {
		m.mu.Unlock()
		return rejected, err
	}
	receipt := rejected
	receipt.Outcome = "unknown"
	m.records[r.OperationID] = startRecord{r, receipt}
	m.command = cmd
	m.childSocket = filepath.Join(m.config.Directory, "managed.sock")
	m.childDone = make(chan struct{})
	m.activeReady = make(chan struct{})
	activeReady := m.activeReady
	childDone := m.childDone
	m.mu.Unlock()
	ready := make(chan struct{})
	go func() {
		var meta struct {
			NodeID string `json:"nodeId"`
			Socket string `json:"socket"`
			State  string `json:"state"`
		}
		d := json.NewDecoder(io.LimitReader(stdout, 16<<10))
		decodeErr := d.Decode(&meta)
		m.mu.Lock()
		record := m.records[r.OperationID]
		if decodeErr == nil && meta.NodeID == r.NodeID && meta.Socket == filepath.Join(m.config.Directory, "managed.sock") && meta.State == "standby" {
			record.receipt.Outcome = "accepted"
			m.records[r.OperationID] = record
		}
		m.mu.Unlock()
		close(ready)
		var active struct {
			Endpoint string `json:"endpoint"`
		}
		_ = d.Decode(&active)
		close(activeReady)
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
		m.mu.Lock()
		if m.command == cmd {
			m.command = nil
			m.childSocket = ""
		}
		close(childDone)
		m.mu.Unlock()
	}()
	select {
	case <-ready:
	case <-ctx.Done():
	case <-time.After(20 * time.Second):
	}
	m.mu.Lock()
	receipt = m.records[r.OperationID].receipt
	m.mu.Unlock()
	return receipt, nil // unknown is retained under the original operation ID
}
func (m *ManagedStarter) child(ctx context.Context) (*Client, error) {
	m.mu.Lock()
	socket := m.childSocket
	m.mu.Unlock()
	if socket == "" || m.life.Err() != nil {
		return nil, errors.New("managed native child unavailable")
	}
	return Dial(ctx, socket, m.config.NodeID)
}
func (m *ManagedStarter) ReadRuntimeProof(ctx context.Context, t api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	c, err := m.child(ctx)
	if err != nil {
		return nodeplane.RuntimeEligibility{}, err
	}
	defer c.Close()
	return c.ReadRuntimeProof(ctx, t)
}
func (m *ManagedStarter) ReadManagedProduct(ctx context.Context, t api.WorkTarget) (ManagedProductEndpoint, error) {
	m.mu.Lock()
	ready := m.activeReady
	m.mu.Unlock()
	if ready == nil {
		return ManagedProductEndpoint{}, errors.New("managed native child unavailable")
	}
	select {
	case <-ready:
	case <-ctx.Done():
		return ManagedProductEndpoint{}, ctx.Err()
	}
	c, err := m.child(ctx)
	if err != nil {
		return ManagedProductEndpoint{}, err
	}
	defer c.Close()
	return c.ReadManagedProduct(ctx, t)
}
func (m *ManagedStarter) PrepareManagedDisable(ctx context.Context, r ManagedDisableRequest) (nodeplane.SnapshotRef, error) {
	c, err := m.child(ctx)
	if err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	defer c.Close()
	return c.PrepareManagedDisable(ctx, r)
}
func (m *ManagedStarter) Close() error {
	m.mu.Lock()
	cmd, done := m.command, m.childDone
	m.mu.Unlock()
	if cmd == nil {
		return nil
	}
	// Graceful cancellation belongs to this exact helper. Native process fencing
	// and its independent parent-death guard own descendants, never a global kill.
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("managed native helper did not confirm shutdown")
	}
}
func (s *Service) ManagedRoamingStatus(ctx context.Context, nodeID string) (ManagedStartStatus, error) {
	if nodeID != s.options.NodeID || s.options.ManagedStart == nil {
		return ManagedStartStatus{}, errors.New("managed native plan unavailable")
	}
	return s.options.ManagedStart.ManagedRoamingStatus(ctx, nodeID)
}
func (s *Service) StartManagedRoaming(ctx context.Context, r ManagedStartRequest) (ManagedStartReceipt, error) {
	if r.NodeID != s.options.NodeID || s.options.ManagedStart == nil {
		return ManagedStartReceipt{}, errors.New("managed native plan unavailable")
	}
	return s.options.ManagedStart.StartManagedRoaming(ctx, r)
}
func (c *Client) ManagedRoamingStatus(ctx context.Context, nodeID string) (ManagedStartStatus, error) {
	var result ManagedStartStatus
	if nodeID != c.expected {
		return result, errors.New("managed node mismatch")
	}
	err := c.request(ctx, "POST", "/v1/node/managed-status", struct {
		NodeID string `json:"nodeId"`
	}{nodeID}, &result)
	if err == nil && result.NodeID != nodeID {
		err = errors.New("managed status node mismatch")
	}
	return result, err
}
func (c *Client) StartManagedRoaming(ctx context.Context, r ManagedStartRequest) (ManagedStartReceipt, error) {
	var result ManagedStartReceipt
	if r.NodeID != c.expected {
		return result, errors.New("managed node mismatch")
	}
	err := c.request(ctx, "POST", "/v1/node/managed-start", r, &result)
	if err == nil && (result.OperationID != r.OperationID || result.NodeID != r.NodeID || result.BotID != r.BotID || result.BrokerNodeID != r.BrokerNodeID) {
		err = errors.New("managed receipt scope mismatch")
	}
	return result, err
}
