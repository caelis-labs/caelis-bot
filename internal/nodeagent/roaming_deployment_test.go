package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func outgoingDeploymentFixture(t *testing.T) (*Service, string) {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "caelis-out-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	if e := os.Chmod(directory, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := New(Options{Directory: directory, NodeID: "nat-node", Join: api.NodeOutgoing, Binaries: map[api.NodeBackend]string{api.NodeCodex: "/usr/bin/true"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(directory, "outgoing-route.json"), OutgoingRoute{Target: "existing-coordinator", Helper: "/paired/caelis-agent", Directory: "/private/joins/nat"}); e != nil {
		t.Fatal(e)
	}
	return s, directory
}
func TestOutgoingDeploymentMetadataOverRealDuplexAndScope(t *testing.T) {
	service, directory := outgoingDeploymentFixture(t)
	helper := filepath.Join(directory, "caelis-node")
	bytes := []byte("#!/bin/sh\nexit 99\n")
	if e := os.WriteFile(helper, bytes, 0700); e != nil {
		t.Fatal(e)
	}
	left, right := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = productrpc.ServeNativeStream(ctx, right, right, Handler(service), allowed)
	}()
	client, e := NewClient("nat-node", left)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { client.Close(); right.Close(); <-done }()
	reply, e := client.RoamingDeployment(ctx, RoamingDeploymentRequest{NodeID: "nat-node", Action: "metadata"})
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(bytes)
	if reply.Metadata == nil || reply.Metadata.Directory != directory || reply.Metadata.Helper != helper || reply.Metadata.HelperSHA256 != hex.EncodeToString(sum[:]) || reply.Metadata.Route.Target != "existing-coordinator" {
		t.Fatal("metadata did not bind actual target-local paths", reply)
	}
	for _, request := range []RoamingDeploymentRequest{
		{NodeID: "another-node", Action: "metadata"},
		{NodeID: "nat-node", Action: "metadata", OperationID: "write"},
		{NodeID: "nat-node", Action: "exec", OperationID: "original", PlanID: strings.Repeat("a", 64)},
		{NodeID: "nat-node", Action: "start", OperationID: "original", PlanID: strings.Repeat("a", 64), Plan: json.RawMessage(`{"shell":"anything"}`)},
	} {
		if _, e = client.RoamingDeployment(ctx, request); e == nil {
			t.Fatal("unclosed request accepted", request)
		}
	}
	if _, e = os.Stat(filepath.Join(directory, "roaming-original")); !os.IsNotExist(e) {
		t.Fatal("metadata wrote deployment")
	}
}
func TestOutgoingCompanionStagesOnlyFixedVerifiedBytesWithoutReplay(t *testing.T) {
	service, directory := outgoingDeploymentFixture(t)
	metadata := RoamingDeploymentMetadata{NodeID: "nat-node", Directory: directory, Helper: filepath.Join(directory, "caelis-node"), OS: "linux", Architecture: "amd64"}
	// Tiny ELF fixture is sufficient to exercise the production verification
	// parser and checksum; it is never executed or advertised as a Runtime.
	elf := make([]byte, 64)
	copy(elf, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	elf[16] = 2
	elf[18] = 62
	elf[20] = 1
	sum := sha256.Sum256(elf)
	request := RoamingDeploymentRequest{NodeID: "nat-node", Action: "helper", OperationID: "original-upload", PlanID: strings.Repeat("a", 64), Companion: &RoamingCompanionChunk{SHA256: hex.EncodeToString(sum[:]), Architecture: "amd64", SourceRevision: strings.Repeat("b", 40), Total: int64(len(elf)), Bytes: elf[:32]}}
	result, e := service.stageRoamingCompanion(t.Context(), request, metadata)
	if e != nil || result.Outcome != "accepted" {
		t.Fatal(result, e)
	}
	if _, e = service.stageRoamingCompanion(t.Context(), request, metadata); e == nil {
		t.Fatal("original chunk replay accepted")
	}
	changed := request
	chunk := *request.Companion
	changed.Companion = &chunk
	chunk.Offset = 32
	chunk.Bytes = elf[32:]
	chunk.SHA256 = strings.Repeat("c", 64)
	if _, e = service.stageRoamingCompanion(t.Context(), changed, metadata); e == nil {
		t.Fatal("artifact identity changed during transfer")
	}
	request.Companion.Offset = 32
	request.Companion.Bytes = elf[32:]
	result, e = service.stageRoamingCompanion(t.Context(), request, metadata)
	if e != nil || result.Outcome != "accepted" {
		t.Fatal(result, e)
	}
	actual, e := os.ReadFile(metadata.Helper)
	if e != nil || string(actual) != string(elf) {
		t.Fatal("fixed companion publication missing", e)
	}
	info, e := os.Lstat(metadata.Helper)
	if e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("companion permissions", e)
	}
	if _, e = service.stageRoamingCompanion(t.Context(), request, metadata); e == nil {
		t.Fatal("published artifact replay accepted")
	}
}
func TestOutgoingDeploymentRejectsArbitraryRuntimePaths(t *testing.T) {
	service, _ := outgoingDeploymentFixture(t)
	request := RoamingDeploymentRequest{Plan: json.RawMessage(`{"Managed":{"Backend":"codex","CodexBinary":"/arbitrary/command"}}`), Workers: json.RawMessage(`{"runtimes":[]}`)}
	if e := service.validateDeploymentRuntime(t.Context(), request); e == nil {
		t.Fatal("paired port accepted an arbitrary executable")
	}
	request.Plan = json.RawMessage(`{"Managed":{"Backend":"codex","CodexBinary":"/usr/bin/true"}}`)
	request.Workers = json.RawMessage(`{"runtimes":[{"backend":"codex","binary":"/arbitrary/worker","store":""}]}`)
	if e := service.validateDeploymentRuntime(t.Context(), request); e == nil {
		t.Fatal("paired port accepted arbitrary Worker executable")
	}
}
