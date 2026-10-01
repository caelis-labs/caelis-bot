package main

import (
	"encoding/json"
	"errors"
	"io"
	"runtime"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

// Read-only packaged artifact preflight. The executable supplies its own pinned
// revision and location; callers cannot supply paths, digests or credentials.
// No profile, Runtime, listener, SSH connection or deployment is opened.
func inspectNodeArtifacts(args []string, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: caelis-node inspect-node-artifacts")
	}
	var result struct {
		SourceRevision   string               `json:"sourceRevision"`
		Artifacts        []nodeagent.Artifact `json:"artifacts"`
		NativeJoinHelper string               `json:"nativeJoinHelper,omitempty"`
		NativeNodeHost   string               `json:"nativeNodeHost,omitempty"`
	}
	for _, arch := range []string{"amd64", "arm64"} {
		artifact, err := app.DefaultNodeAgentArtifact(arch)
		if err != nil {
			return err
		}
		result.SourceRevision = artifact.SourceRevision
		result.Artifacts = append(result.Artifacts, artifact)
	}
	if runtime.GOOS == "darwin" {
		var err error
		result.NativeJoinHelper, err = app.DefaultNativeJoinHelper()
		if err != nil {
			return err
		}
		result.NativeNodeHost, err = app.DefaultNativeNodeHost()
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(result)
}
