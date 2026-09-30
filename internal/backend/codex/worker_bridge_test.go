package codex

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func TestWorkerBridgeForeignNativeSourceReceiptsDetachAndResources(t *testing.T) {
	d := workerPair(t)
	d.w.source = workerwire.SourceProvider()
	owner := nodeworker.New(d.w)
	pair := workerwire.Pair{Target: d.w.target, BotID: "paired-product-bot", SourceNode: "host-node", SourceBackend: "codex"}
	d.w.pair = pair
	if err := os.Chmod(d.w.engine.opts.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := workerwire.BindPair(d.w.engine.opts.Directory, pair, false); err != nil {
		t.Fatal(err)
	}
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		t.Fatal(err)
	}
	connect := func() *workerwire.Client {
		a, b := net.Pipe()
		go func() { _ = server.Serve(testContext(t), b) }()
		c, err := workerwire.NewClient(testContext(t), pair, d.source, a)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		return c
	}
	client := connect()
	id := "task-" + strings.Repeat("1", 32)
	workspace, err := client.ResolveWorkWorkspace(testContext(t), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = client.PrepareWorkWorkspace(testContext(t), id, workspace, false); err != nil {
		t.Fatal(err)
	}
	source, _ := d.source.WorkDispatchSource(testContext(t))
	target := d.w.target
	in := api.WorkStart{TaskStart: api.TaskStart{RequestID: "paired-start", Title: "Native wire fixture", Prompt: "Synthetic file only", Target: &target}, ID: id, Workspace: workspace, Instructions: botpolicy.WorkerInstructions, Source: source, RequestDigest: strings.Repeat("a", 64)}
	v, err := client.StartWork(testContext(t), in)
	if err != nil || v.Outcome != "accepted" {
		t.Fatal(v, err)
	}
	if len(client.WorkStates()) != 1 || client.WorkStates()[0].Target != target {
		t.Fatal("wire lost native task binding")
	}
	client.WorkStates()[0].Task.Target.NodeID = "caller-mutated"
	if client.WorkStates()[0].Task.Target.NodeID != target.NodeID {
		t.Fatal("cached authority was mutable")
	}
	d.source.mu.Lock()
	d.source.err = errors.New("host native activation ended")
	d.source.mu.Unlock()
	client.Close()
	client = connect()
	if d.stops.Load() != 0 {
		t.Fatal("SSH detach stopped target native process")
	}
	if original, err := client.StartWork(testContext(t), in); err != nil || original.ID != id {
		t.Fatal("original native receipt required new host activation", original, err)
	}
	msg := api.TaskMessage{ID: id, RequestID: in.RequestID, Prompt: in.Prompt, Source: in.Source, RequestDigest: in.RequestDigest}
	if !client.WorkMessageRecorded(msg) {
		t.Fatal("original message receipt lost across detach")
	}
	msg.RequestID = "new-native-request"
	if _, err = client.SendWork(testContext(t), msg); err == nil {
		t.Fatal("ended host activation dispatched new work")
	}
	if _, err = client.ReadWork(testContext(t), "foreign-native-thread"); err == nil {
		t.Fatal("arbitrary thread was adopted as task")
	}
	// Native fileChange owns artifacts. A markdown link cannot grant access.
	file := filepath.Join(workspace, "result.txt")
	if err = os.WriteFile(file, []byte("synthetic bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	d.w.engine.mu.Lock()
	task := d.w.engine.binding.Tasks[id]
	thread, run := task.Thread, task.Run
	d.w.engine.mu.Unlock()
	turn := nativeTurn{ID: run, Status: "completed", Items: []nativeItem{{ID: "agent", Type: "agentMessage", Text: "[private](../secret.txt)"}, {ID: "file", Type: "fileChange", Status: "completed", Changes: []nativeChange{{Path: file}, {Path: filepath.Join(workspace, "../secret.txt")}}}}}
	d.f.mu.Lock()
	native := d.f.workers[thread]
	native.Turns = []nativeTurn{turn}
	native.Status.Type = "idle"
	d.f.workers[thread] = native
	d.f.mu.Unlock()
	if _, err = client.ReadWork(testContext(t), id); err != nil {
		t.Fatal(err)
	}
	refs := client.WorkArtifacts()
	if len(refs) != 1 || refs[0].TaskID != id || refs[0].Artifact.Name != "result.txt" {
		t.Fatal("artifact catalog inferred prose or outside file", refs)
	}
	artifact, err := client.ReadWorkArtifact(testContext(t), id, refs[0].Artifact.ID)
	if err != nil || string(artifact.Bytes) != "synthetic bytes" {
		t.Fatal("bounded native artifact bytes lost", err)
	}
	if _, err = client.ReadWorkArtifact(testContext(t), "other-task", refs[0].Artifact.ID); err == nil {
		t.Fatal("artifact switched task")
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err = os.WriteFile(outside, []byte("unowned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ReadWorkArtifact(testContext(t), id, refs[0].Artifact.ID); err == nil {
		t.Fatal("projected file redirected outside task root")
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 1 {
		t.Fatal("wire reconcile resent native work", starts, sends)
	}
	client.Close()
	if d.stops.Load() != 0 {
		t.Fatal("closing wire observer stopped native owner")
	}
	_ = owner.Stop(context.Background())
}

func TestWorkerPairAssemblyRejectsOriginReuseBeforeNativeConnect(t *testing.T) {
	d := workerPair(t)
	pair := workerwire.Pair{Target: d.w.target, BotID: "origin-bot", SourceNode: "host-node", SourceBackend: "codex"}
	if _, err := workerwire.NewServer(nodeworker.New(d.w), pair); err == nil {
		t.Fatal("wire server adopted an unpaired native owner")
	}
	directory := filepath.Join(t.TempDir(), "private-worker")
	w := NewWorker(WorkerOptions{Target: d.w.target, Directory: directory, Pair: &pair, Source: workerwire.SourceProvider()})
	w.open = d.w.open
	if err := w.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close(testContext(t)) })
	if _, err := workerwire.NewServer(nodeworker.New(w), pair); err != nil {
		t.Fatal("configured native origin not exposed", err)
	}
	changed := pair
	changed.BotID = "other-bot"
	if _, err := workerwire.NewServer(nodeworker.New(w), changed); err == nil {
		t.Fatal("server pairing changed native owner")
	}
	other := NewWorker(WorkerOptions{Target: d.w.target, Directory: directory, Pair: &changed, Source: workerwire.SourceProvider()})
	other.open = func(context.Context, Options) (*Client, func(), string, error) {
		t.Error("pair drift reached native connection")
		return nil, nil, "", errors.New("unexpected connection")
	}
	if err := other.Connect(testContext(t)); err == nil {
		t.Fatal("restart changed originating Bot")
	}
	if _, _, err := d.start(t, 7); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d.w.engine.opts.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	old := NewWorker(WorkerOptions{Target: d.w.target, Directory: d.w.engine.opts.Directory, Pair: &pair, Source: workerwire.SourceProvider()})
	if err := old.Connect(testContext(t)); err == nil {
		t.Fatal("source-less retained native tasks automatically paired")
	}
}
