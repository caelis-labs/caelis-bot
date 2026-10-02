//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"golang.org/x/sys/unix"
)

type notebookStopEngine struct {
	*updateEngine
	fences atomic.Int32
}

func (e *notebookStopEngine) FenceStop(context.Context) error {
	e.fences.Add(1)
	return errors.New("fixture dispatched stop with lost native receipt")
}

// Exercise the actual candidate notebook-owner CLI through a contained SSH
// process, with a real RPC owner and the production APP StopSource hook. Only
// the native engine and the SSH machine boundary are synthetic.
func TestNotebookRemoteStopProductionChain(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_COMPOSITION_HELPER")
	if helper == "" {
		t.Skip("requires explicit candidate native helper; no real nodes")
	}
	for _, busy := range []bool{true, false} {
		t.Run(map[bool]string{true: "not-dispatched", false: "dispatched-unknown"}[busy], func(t *testing.T) {
			engine := &notebookStopEngine{updateEngine: &updateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Connection: "ready", CanInterrupt: busy}}}
			source, _ := fixtureApp(t, engine, Host{})
			source.started = true
			c, profile, _ := returnFixture(t)
			directory := filepath.Dir(profile)
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(profile, 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := nodeagent.New(nodeagent.Options{Directory: directory, NodeID: "remote", Join: api.NodeSSH}); err != nil {
				t.Fatal(err)
			}
			write := func(path string, v any) {
				t.Helper()
				if err := localstate.Write(path, v); err != nil {
					t.Fatal(err)
				}
			}
			botBytes, err := os.ReadFile(filepath.Join(c.app.root, "bot.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(profile, "bot.json"), botBytes, 0600); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(filepath.Join(profile, ".product-owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			stopped := make(chan productrpc.Result, 1)
			token := strings.Repeat("synthetic-owner-token-", 3)
			var host *httptest.Server
			server, err := productrpc.NewServer(productrpc.ServicePort{Service: source.Backend, Stop: source.StopNotebookOwner}, productrpc.Options{Context: t.Context(), NodeID: "remote", BotID: c.app.product.pairing.BotID, Token: token, JournalFile: filepath.Join(source.root, "receipts.json"), OnStopped: func(result productrpc.Result) {
				_ = host.Listener.Close()
				_ = source.Close() // the production serve owner's deferred cleanup
				stopped <- result
			}})
			if err != nil {
				t.Fatal(err)
			}
			host = httptest.NewServer(server)
			defer host.Close()
			c.app.product.pairing.Endpoint = host.URL
			state := nodeagent.NotebookOwnerState{Endpoint: host.URL, Identity: server.Identity()}
			write(filepath.Join(profile, "Product", "owner.json"), state)
			if err = os.WriteFile(filepath.Join(profile, "Product", "product.auth"), []byte(token), 0600); err != nil {
				t.Fatal(err)
			}
			tools := t.TempDir()
			script := "#!/bin/sh\nif [ \"$1\" = -G ]; then printf 'hostname fixture\\nuser fixture\\nport 22\\nuserknownhostsfile /dev/null\\nglobalknownhostsfile /dev/null\\n'; exit 0; fi\nwhile [ \"$#\" -gt 0 ]; do case \"$1\" in -o|-F) shift 2;; -T) shift;; --) shift; break;; *) break;; esac; done\n[ \"$1\" = fixture ] || exit 2\nshift\nexec /bin/sh -c \"$*\"\n"
			if err = os.WriteFile(filepath.Join(tools, "ssh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools+":/usr/bin:/bin")
			c.ownerCall = func(ctx context.Context, r NodeRegistration, in nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error) {
				return nodeagent.NotebookOwner(ctx, nodeagent.SSHConfig{Target: r.SSHDestination}, helper, r.Directory, r.ID, in)
			}
			opts, err := c.options(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			hooks := opts.Hooks
			hooks.TargetReady = nil // the retained local standby is already prepared
			hooks.Transfer = func(context.Context, string, bool) error {
				t.Error("unconfirmed stop reached transfer")
				return errors.New("unexpected transfer")
			}
			hooks.Save = func(s notebooksync.State) error {
				return localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-sync.json"), s)
			}
			controller, err := notebooksync.New(notebooksync.State{SourceNodeID: "remote", Targets: []notebooksync.Status{{NodeID: "local", Phase: "ready"}}}, hooks)
			if err != nil {
				t.Fatal(err)
			}
			c.app.notebookSync = controller
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err = controller.Switch(ctx, "local")
			if err == nil {
				t.Fatal("unconfirmed stop accepted")
			}
			if busy {
				if !errors.Is(err, api.ErrStopNotDispatched) || controller.State().Targets[0].Phase != "ready" || engine.fences.Load() != 0 || source.sourceRetired {
					t.Fatal("pre-dispatch rejection lost", err, controller.State())
				}
				select {
				case result := <-stopped:
					t.Fatal("rejected stop retired owner", result)
				default:
				}
				if engine.closed != 0 {
					t.Fatal("busy owner closed")
				}
				if err = c.app.PrepareUpdate(); err != nil {
					t.Fatal("client admission still frozen", err)
				}
				c.app.CancelUpdate()
				client, err := productrpc.NewClient(productrpc.ClientOptions{URL: host.URL, ExpectedNode: "remote", ExpectedBot: state.Identity.BotID, Token: token})
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				if _, err = client.Connect(ctx); err != nil {
					t.Fatal("owner listener closed", err)
				}
				id := "notebook-stop-" + controller.State().Targets[0].OperationID
				result, err := client.Receipt(ctx, id)
				if err != nil || result.Outcome != "rejected" || result.Code != productrpc.StopNotDispatchedCode {
					t.Fatal("original rejection not durable", result, err)
				}
				observed, readErr := client.State(ctx)
				if readErr != nil {
					t.Fatal(readErr)
				}
				draft := observed.Draft
				draft.Text = "after rejected stop"
				if result, err = client.Command(ctx, productrpc.Command{ID: "after-rejection", Kind: "save-draft", Draft: &draft}); err != nil || result.Outcome != "accepted" {
					t.Fatal("server remained stopping", result, err)
				}
			} else {
				if errors.Is(err, api.ErrStopNotDispatched) || controller.State().Targets[0].Phase != "stopping" || engine.fences.Load() != 1 {
					t.Fatal("dispatched uncertainty was washed away", err, controller.State())
				}
				select {
				case result := <-stopped:
					if result.Outcome != "unknown" {
						t.Fatal(result)
					}
				case <-ctx.Done():
					t.Fatal("native stop attempt lost owner shutdown notification")
				}
				if err = c.app.PrepareUpdate(); err == nil {
					t.Fatal("unknown stop reopened client admission")
				}
			}
		})
	}
}
