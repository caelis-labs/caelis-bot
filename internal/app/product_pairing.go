package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type productPairingDocument struct {
	Version int                    `json:"version"`
	Pairing backend.ProductPairing `json:"pairing"`
}

func loadProductPairing(root string) (backend.ProductPairing, error) {
	filename := filepath.Join(root, "product-connection.json")
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return backend.ProductPairing{Mode: "local"}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32<<10 {
		return backend.ProductPairing{}, errors.New("private product pairing cannot be read; original configuration was preserved")
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return backend.ProductPairing{}, errors.New("private product pairing cannot be read")
	}
	var document productPairingDocument
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || document.Version != 1 {
		return backend.ProductPairing{}, errors.New("private product pairing is invalid; original configuration was preserved")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || validateProductPairing(document.Pairing) != nil {
		return backend.ProductPairing{}, errors.New("private product pairing is invalid; original configuration was preserved")
	}
	return document.Pairing, nil
}

// RemoteProductSelected lets native bootstrap skip local runtime environment
// discovery before constructing an explicitly selected thin APP.
func RemoteProductSelected(root string) (bool, error) {
	pairing, err := loadProductPairing(root)
	return pairing.Mode == "remote", err
}

var productIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func validateProductPairing(p backend.ProductPairing) error {
	if p.Mode == "local" {
		return nil
	}
	if p.Mode != "remote" || strings.TrimSpace(p.Label) == "" || len(p.Label) > 128 || strings.ContainsAny(p.Label, "\x00\r\n") || !workerSSH.MatchString(p.SSH) || strings.HasPrefix(p.SSH, "-") || len(p.SSH) > 256 || !productIdentifier.MatchString(p.NodeID) || !productIdentifier.MatchString(p.BotID) {
		return errors.New("product pairing identity is invalid")
	}
	if p.Helper != "" && (len(p.Helper) > 4096 || strings.ContainsAny(p.Helper, " \t\r\n\x00") || strings.HasPrefix(p.Helper, "-") || strings.Contains(p.Helper, "..")) {
		return errors.New("product proxy executable is invalid")
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || !net.ParseIP(u.Hostname()).IsLoopback() || u.Port() == "" {
		return errors.New("product endpoint must be target-local loopback")
	}
	if !path.IsAbs(p.AuthFile) || path.Clean(p.AuthFile) != p.AuthFile || len(p.AuthFile) > 4096 || strings.ContainsAny(p.AuthFile, "\x00\r\n") {
		return errors.New("product auth file must be a target-local absolute path")
	}
	return nil
}

type productPairingController struct {
	mu              sync.Mutex
	filename        string
	pairing, active backend.ProductPairing
	revision        uint64
	remote          *productEngine
}

func newProductPairingController(root string, pairing backend.ProductPairing, remote *productEngine) *productPairingController {
	return &productPairingController{filename: filepath.Join(root, "product-connection.json"), pairing: pairing, active: pairing, revision: 1, remote: remote}
}

func (c *productPairingController) ConnectionState() backend.ProductConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := backend.ProductConnectionState{Revision: c.revision, Pairing: c.pairing, ActiveMode: c.active.Mode, State: "local", RestartRequired: c.pairing != c.active}
	if c.remote != nil {
		state.State, state.Issue = c.remote.connectionState()
	}
	return state
}

func (c *productPairingController) SavePairing(pairing backend.ProductPairing, revision uint64) (backend.ProductConnectionState, error) {
	if err := validateProductPairing(pairing); err != nil {
		return c.ConnectionState(), err
	}
	if pairing.Mode == "local" {
		pairing = backend.ProductPairing{Mode: "local"}
	}
	c.mu.Lock()
	if revision != c.revision {
		c.mu.Unlock()
		return c.ConnectionState(), errors.New("product pairing changed; refresh before saving")
	}
	if err := localstate.Write(c.filename, productPairingDocument{Version: 1, Pairing: pairing}); err != nil {
		c.mu.Unlock()
		return c.ConnectionState(), errors.New("product pairing could not be saved")
	}
	c.pairing = pairing
	c.revision++
	c.mu.Unlock()
	return c.ConnectionState(), nil
}

func (c *productPairingController) Reconnect(ctx context.Context) error {
	if c.remote == nil {
		return errors.New("restart APP to connect the selected remote Bot")
	}
	return c.remote.Connect(ctx)
}

func (c *productPairingController) Disconnect(ctx context.Context) error {
	if c.remote == nil {
		return errors.New("APP currently uses its local Bot")
	}
	return c.remote.detach(ctx)
}

var _ backend.ProductConnectionController = (*productPairingController)(nil)
