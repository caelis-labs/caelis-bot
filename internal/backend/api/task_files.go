package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// TaskFileReference is either an artifact of an owned Worker or an explicitly
// selected artifact of A's resident conversation (empty TaskID). No remote path.
type TaskFileReference struct {
	TaskID string `json:"taskId,omitempty"`
	ID     string `json:"id"`
}
type TaskFileReader interface {
	ReadTaskFile(context.Context, string, string) (WorkFileContent, error)
}
type TaskObserver interface {
	ObserveTask(context.Context, string) (WorkObservation, error)
}

func TaskArtifactID(task, id string) string {
	b, _ := json.Marshal(TaskFileReference{task, id})
	return "task-file:" + base64.RawURLEncoding.EncodeToString(b)
}
func ParseTaskArtifactID(id string) (TaskFileReference, bool) {
	if !strings.HasPrefix(id, "task-file:") {
		return TaskFileReference{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "task-file:"))
	var out TaskFileReference
	if err != nil || json.Unmarshal(b, &out) != nil || out.TaskID == "" || out.ID == "" {
		return TaskFileReference{}, false
	}
	return out, true
}

type TaskOperationReader interface {
	ReconcileTaskRequest(context.Context, string, string) (WorkOperation, error)
}

// WorkerTaskObservation is the renderer-safe output view. Native owner facts,
// complete pending requests and async reply identities stay in the host ports.
type WorkerTaskObservation struct {
	Task     Task   `json:"task"`
	Items    []Item `json:"items"`
	Revision string `json:"revision"`
}
type WorkerTaskViewer interface {
	ListTasks() []Task
	ReadTaskConversation(context.Context, string) (TaskConversation, error)
	ObserveTask(context.Context, string) (WorkObservation, error)
}
