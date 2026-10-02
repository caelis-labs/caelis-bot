//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// connect-enrolled-worker is an explicit native Connect action. The ordinary
// serve-worker process owns its complete target-local Runtime independently of
// this SSH observer; closing the stream never stops or replays a task.
func connectEnrolledWorker(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	f := flag.NewFlagSet("connect-enrolled-worker", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	directory := f.String("directory", "", "existing native enrollment")
	node := f.String("node-id", "", "exact physical Node")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return errors.New("exact enrolled Worker arguments required")
	}
	if _, err := nodeagent.ReadNativeEnrollmentIdentity(*directory, *node); err != nil {
		return err
	}
	buffered := bufio.NewReaderSize(in, 16<<10)
	line, err := buffered.ReadSlice('\n')
	if err != nil {
		return errors.New("bounded exact Worker pairing required")
	}
	var pair workerwire.Pair
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pair) != nil || decoder.Decode(new(any)) != io.EOF || workerwire.ValidateRelayPair(pair) != nil || pair.Target.NodeID != *node || pair.Target.Backend != "codex" {
		return errors.New("ordinary Worker pairing invalid")
	}
	socket, err := ensureEnrolledWorker(ctx, *directory, pair)
	if err != nil {
		return err
	}
	return workerwire.ProxyUnix(ctx, buffered, out, socket)
}
func enrolledWorkerDirectory(directory string, pair workerwire.Pair) string {
	key, _ := json.Marshal(pair)
	sum := sha256.Sum256(key)
	return filepath.Join(directory, "w"+hex.EncodeToString(sum[:])[:12])
}
func ensureEnrolledWorker(ctx context.Context, directory string, pair workerwire.Pair) (string, error) {
	root := enrolledWorkerDirectory(directory, pair)
	if err := prepareWorkerDirectory(root); err != nil {
		return "", err
	}
	release, err := lockProfile(filepath.Join(root, ".worker-control.lock"))
	if err != nil {
		return "", err
	}
	defer release()
	socket := filepath.Join(root, "worker.sock")
	if len(socket) >= 100 {
		return "", errors.New("enrolled Worker path exceeds private IPC limit")
	}
	if err := workerwire.BindPair(root, pair, false); err != nil {
		return "", err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !workerFileOwner(info) {
			return "", errors.New("existing Worker endpoint unavailable")
		}
		return socket, nil // The client's exact native hello verifies owner and pair.
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	marker := filepath.Join(root, "owner-start.json")
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("original Worker start unconfirmed; inspect native owner")
	}
	options := nodeagent.Options{Directory: directory, NodeID: pair.Target.NodeID, Label: "Runtime node", Join: api.NodeSSH, Configurations: map[api.NodeBackend]nodeagent.NativeConfiguration{api.NodeCodex: &nodeagent.CodexConfiguration{Directory: directory}}}
	if runtime.GOOS == "linux" {
		options.RuntimeDirectory = filepath.Join(directory, "runtime")
	}
	service, err := nodeagent.New(options)
	if err != nil {
		return "", err
	}
	settings, err := service.ReadOwnedRuntimeSettings(ctx, pair.Target.NodeID, api.NodeCodex)
	if err != nil {
		return "", err
	}
	preferences, err := nodeagent.ReadExecutionPreferences(directory)
	if err != nil {
		return "", err
	}
	config := workerConfiguration{Version: 1, Pair: pair, Directory: root, Binary: settings.Binary, Socket: socket, Execution: preferences.Worker}
	file := filepath.Join(root, "worker.json")
	if err = localstate.Write(file, config); err != nil {
		return "", err
	}
	if _, err = readWorkerConfiguration(file); err != nil {
		return "", err
	}
	helper, err := os.Executable()
	if err != nil {
		return "", err
	}
	log, err := os.OpenFile(filepath.Join(root, "owner.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer log.Close()
	command := exec.Command(helper, "serve-worker", "--config-file", file)
	command.Stdout, command.Stderr = log, log
	command.Env = notebookOwnerEnvironment()
	if err = localstate.Write(marker, map[string]string{"phase": "starting"}); err != nil {
		return "", err
	}
	if err = detachNotebookOwner(command); err != nil {
		return "", err
	}
	if err = localstate.Write(marker, map[string]string{"phase": "started", "pid": strconv.Itoa(command.Process.Pid)}); err != nil {
		return "", err
	}
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0600 && workerFileOwner(info) {
			return socket, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("ordinary native Worker start unconfirmed")
		case <-tick.C:
		}
	}
}
