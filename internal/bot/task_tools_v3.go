package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (r *Runtime) taskPort() (api.TaskProvider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.tasks == nil {
		return nil, errors.New("task Runtime is unavailable")
	}
	return r.tasks, nil
}

func toolSubmissionID(invocation api.ToolInvocation) string {
	sum := sha256.Sum256([]byte(invocation.Provider + "\x00" + invocation.Session + "\x00" + invocation.Turn + "\x00" + invocation.CallID))
	return "call-" + hex.EncodeToString(sum[:20])
}

func (r *Runtime) reserveDelegation(id string, args map[string]any) error {
	encoded, _ := json.Marshal(args)
	sum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(sum[:])
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.state.Delegations[id]; ok {
		if old.Fingerprint != fingerprint {
			return errors.New("original tool invocation already has a different task or prompt")
		}
		return nil
	}
	if r.state.Delegations == nil {
		r.state.Delegations = map[string]delegationIntent{}
	}
	r.state.Delegations[id] = delegationIntent{Fingerprint: fingerprint, Task: str(args, "task")}
	if err := r.saveLocked(); err != nil {
		delete(r.state.Delegations, id)
		return err
	}
	return nil
}

func taskReceipt(task api.Task) map[string]any {
	data := map[string]any{"id": task.ID, "status": task.Status}
	if task.Title != "" {
		data["title"] = task.Title
	}
	if task.Outcome != "" {
		data["outcome"] = task.Outcome
	}
	return data
}

func (r *Runtime) callDelegate(ctx context.Context, q map[string]any) api.ToolResult {
	invocation, ok := api.ToolInvocationFromContext(ctx)
	if !ok || invocation.Provider != r.provider || len(invocation.CallID) > 256 {
		return compactError("invocation_unavailable", errors.New("original resident tool invocation is unavailable; no Worker submission was attempted"))
	}
	provider, err := r.taskPort()
	if err != nil {
		return compactError("runtime_unavailable", err)
	}
	taskID := str(q, "task")
	if taskID != "" {
		for _, key := range []string{"title", "workspace", "environment"} {
			if _, supplied := q[key]; supplied {
				return compactError("invalid_arguments", errors.New("title, workspace and environment are creation-only; this task keeps its original binding"))
			}
		}
	}
	environment := str(q, "environment")
	if environment == "" {
		environment = "local"
	}
	machine := ""
	if taskID == "" && environment != "local" {
		machines, ok := provider.(api.TaskMachineProvider)
		if !ok {
			return compactError("environment_unavailable", errors.New("remote execution environments are unavailable"))
		}
		found := false
		for _, target := range machines.TaskMachines() {
			if target.ID == environment {
				found = target.Ready
				break
			}
		}
		if !found {
			return compactError("environment_unavailable", errors.New("selected environment is not connected; list environments and retain the original task intent"))
		}
		machine = environment
	}
	requestID := toolSubmissionID(invocation)
	if err := r.reserveDelegation(requestID, q); err != nil {
		return compactError("invocation_conflict", err)
	}
	var task api.Task
	var attachments []api.TaskFileReference
	if raw, ok := q["attachments"]; ok {
		b, _ := json.Marshal(raw)
		if json.Unmarshal(b, &attachments) != nil {
			return compactError("invalid_attachments", errors.New("invalid Worker file references"))
		}
	}
	// Byte transfer shares the original invocation, but is not constrained to
	// the small text-admission timeout. Cancellation and native reconciliation
	// remain unchanged; no second upload or write retry is introduced.
	deadline := api.WorkerAdmissionTimeout
	if len(attachments) > 0 {
		deadline = api.WorkerAttachmentTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if taskID == "" {
		task, err = provider.StartTask(bounded, api.TaskStart{RequestID: requestID, Prompt: str(q, "prompt"), Title: str(q, "title"), Workspace: str(q, "workspace"), Machine: machine, LocalRuntime: machine == "", Attachments: attachments})
	} else {
		task, err = provider.SendTask(bounded, api.TaskMessage{ID: taskID, RequestID: requestID, Prompt: str(q, "prompt"), Attachments: attachments})
	}
	data := taskReceipt(task)
	data["requestId"] = requestID
	if err != nil || task.Status == "unknown" || task.Outcome == "unknown" || task.Outcome == "rejected" || task.Outcome == "conflicted" {
		code, message := "unconfirmed", "The original Worker submission is not confirmed. The Host retained its original receipt and will not replay it with a new ID."
		var rejected *api.WorkMessageRejection
		reason, detail := "", ""
		if task.Outcome == "rejected" || task.Outcome == "conflicted" {
			if errors.As(err, &rejected) {
				reason = rejected.Code
				detail = api.WorkRejectionDetail(rejected.Cause)
				code, message = "submission_rejected", delegationRejectionGuidance(reason)
				if task.Outcome == "conflicted" {
					code, message = "submission_conflicted", delegationRejectionGuidance("native_conflict")
				}
			} else if err == nil {
				code, reason = "submission_rejected", "native_rejected"
				if task.Outcome == "conflicted" {
					code, reason = "submission_conflicted", "native_conflict"
				}
				message = delegationRejectionGuidance(reason)
				detail = api.WorkRejectionDetail(nil)
			}
		}
		data["reason"], data["detail"] = reason, detail
		return compactValue(map[string]any{"ok": false, "outcome": task.Outcome, "data": data, "error": map[string]string{"code": code, "message": message, "reason": reason, "detail": detail}}, true)
	}
	return compactValue(map[string]any{"ok": true, "data": data}, false)
}

func delegationRejectionGuidance(reason string) string {
	switch reason {
	case "updater_paused":
		return "Worker admission is temporarily paused for an update. Retry later, after at least 30 seconds; retain the original request and task."
	case "capacity_full":
		return "Worker capacity is temporarily full. Retry later, after at least 30 seconds or when active work finishes; retain the original request and task."
	case "work_admission_failed":
		return "Worker admission is temporarily unavailable. Retry later, after at least 30 seconds; retain the original request and task."
	case "workspace_invalid":
		return "The workspace was rejected before Worker input was accepted. Use an existing project directory outside the Bot private Notebook, or omit workspace for an isolated task directory."
	case "native_conflict":
		return "The original Worker submission conflicted with its native owner. Inspect the original task and request before another action."
	case "native_rejected":
		return "The native owner rejected this Worker submission. Inspect the original task and refusal before another action; do not bypass a denied approval."
	default:
		return "This Worker input was rejected. Inspect the original task and refusal before another action; no retry guidance is available."
	}
}

func taskEnvironments(provider api.TaskProvider, localStatus string) []map[string]string {
	if localStatus == "ready" {
		localStatus = "connected"
	}
	if localStatus == "" {
		localStatus = "unavailable"
	}
	out := []map[string]string{{"key": "local", "name": "This Bot Runtime", "status": localStatus}}
	if machines, ok := provider.(api.TaskMachineProvider); ok {
		for _, target := range machines.TaskMachines() {
			if target.ID == "" || target.ID == "local" {
				continue
			}
			status := "unavailable"
			if target.Ready {
				status = "connected"
			}
			out = append(out, map[string]string{"key": target.ID, "name": target.Name, "status": status})
		}
	}
	sort.Slice(out[1:], func(i, j int) bool { return out[i+1]["key"] < out[j+1]["key"] })
	return out
}

func collapsedTaskText(value string) string {
	if len(value) <= 1000 {
		return value
	}
	front := 700
	for front > 0 && !utf8.RuneStart(value[front]) {
		front--
	}
	back := len(value) - 300
	for back < len(value) && !utf8.RuneStart(value[back]) {
		back++
	}
	if back <= front {
		return value
	}
	return value[:front] + fmt.Sprintf("[已折叠 %d bytes]", back-front) + value[back:]
}

func fullTaskValue(value map[string]any, q map[string]any) api.ToolResult {
	encoded, _ := json.Marshal(value)
	if len(encoded) > 7<<20 {
		return compactError("message_too_large", errors.New("original native message exceeds the bounded tool transport; open the task in its native client"))
	}
	return boundedData(value, q)
}

func (r *Runtime) callTasksV3(ctx context.Context, q map[string]any) api.ToolResult {
	provider, err := r.taskPort()
	if err != nil {
		return compactError("runtime_unavailable", err)
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch str(q, "type") {
	case "ssh_hosts":
		p, ok := provider.(interface{ SSHHosts() (api.SSHHosts, error) })
		if !ok {
			return compactError("unavailable", errors.New("SSH hosts unavailable"))
		}
		v, e := p.SSHHosts()
		if e != nil {
			return compactError("unavailable", e)
		}
		return boundedData(map[string]any{"ok": true, "data": v}, q)
	case "environments":
		return boundedData(map[string]any{"ok": true, "data": taskEnvironments(provider, r.engine.Snapshot().Connection)}, q)
	case "list":
		catalog, ok := provider.(api.TaskHistoryCatalog)
		if !ok {
			return compactError("unavailable", errors.New("task history catalog unavailable"))
		}
		query := api.TaskHistoryQuery{Query: str(q, "query"), Status: str(q, "status"), CreatedAfter: str(q, "createdAfter"), CreatedBefore: str(q, "createdBefore"), Order: str(q, "order")}
		if value, ok := q["offset"].(float64); ok {
			query.Offset = int(value)
		}
		if value, ok := q["limit"].(float64); ok {
			query.Limit = int(value)
		}
		page, err := catalog.QueryTaskHistory(query)
		if err != nil {
			return compactError("invalid_or_unavailable_query", err)
		}
		end := 0
		for end < len(page) {
			if end > 0 && len(encoded(page[:end+1])) > 16<<10 {
				break
			}
			end++
		}
		data := map[string]any{"tasks": page[:end]}
		limit := query.Limit
		if limit == 0 {
			limit = 20
		}
		if end < len(page) || len(page) == limit {
			data["nextOffset"] = query.Offset + end
		}
		return compactValue(map[string]any{"ok": true, "data": data}, false)
	case "read":
		reader, ok := provider.(api.TaskConversationReader)
		if !ok {
			return compactError("unavailable", errors.New("native Worker message reading unavailable"))
		}
		if _, detail := q["messageId"]; detail {
			if _, paged := q["offset"]; paged {
				return compactError("invalid_arguments", errors.New("messageId cannot be combined with offset or limit"))
			}
			if _, paged := q["limit"]; paged {
				return compactError("invalid_arguments", errors.New("messageId cannot be combined with offset or limit"))
			}
		}

		conversation, err := reader.ReadTaskConversation(bounded, str(q, "task"))
		if err != nil {
			return compactError("native_history_unreadable", errors.New("original Worker messages could not be read; retry this same task after the Runtime reconnects"))
		}
		if id := str(q, "messageId"); id != "" {
			for _, message := range conversation.Messages {
				if message.ID == id {
					data := map[string]any{"status": conversation.Status, "message": message}
					return taskReadValue(data, conversation.Failure, q)
				}
			}
			return compactError("message_not_found", errors.New("messageId is not a completed User or Assistant message in this task"))
		}
		offset, limit := 0, 10
		if value, ok := q["offset"].(float64); ok {
			offset = int(value)
		}
		if value, ok := q["limit"].(float64); ok {
			limit = int(value)
		}
		messages := []api.TaskConversationMessage{}
		if offset < len(conversation.Messages) {
			messages = append(messages, conversation.Messages[offset:min(len(conversation.Messages), offset+limit)]...)
		}
		for i := range messages {
			messages[i].Text = collapsedTaskText(messages[i].Text)
		}
		return taskReadValue(map[string]any{"status": conversation.Status, "messages": messages}, conversation.Failure, q)
	case "observe":
		p, ok := provider.(api.TaskObserver)
		if !ok {
			return compactError("unavailable", errors.New("Worker observation unavailable"))
		}
		v, err := p.ObserveTask(bounded, str(q, "task"))
		if err != nil {
			return compactError("original_owner_unavailable", err)
		}
		data := map[string]any{"task": v.State.Task, "items": v.Items, "finishToken": api.TaskFinishToken(v.State)}
		// Adapter counters may advance on a read with identical native facts.
		// Page the content revision; keep the observer counter outside chunks.
		data["revision"] = interactionFingerprint(data)
		value := map[string]any{"ok": true, "data": data, "nativeRevision": v.Revision}
		if v.State.Task.Failure != nil {
			value["failure"] = v.State.Task.Failure
		}
		return fullTaskValue(value, q)
	case "reconcile":
		p, ok := provider.(api.TaskOperationReader)
		if !ok {
			return compactError("unavailable", api.ErrWorkOperationUnsupported)
		}
		v, err := p.ReconcileTaskRequest(bounded, str(q, "task"), str(q, "requestId"))
		if err != nil {
			return compactError("original_operation_unavailable", err)
		}
		return compactValue(map[string]any{"ok": true, "data": v}, false)
	case "file":
		if p, ok := provider.(api.TaskFileMaterializer); ok {
			v, err := p.MaterializeTaskFile(bounded, str(q, "task"), str(q, "file"), true)
			if err != nil {
				return compactError("native_file_unavailable", err)
			}
			return compactValue(map[string]any{"ok": true, "data": map[string]any{"artifact": api.Artifact{ID: v.ID, Name: v.Name}, "localPath": v.Path, "sha256": v.SHA256, "bytes": v.Bytes, "mediaType": v.MediaType, "cached": v.Cached, "fetchedAt": v.FetchedAt, "usage": "Read this checked A-local copy with native file/image tools. Cached bytes are the last retrieved version, not proof of the remote latest content."}}, false)
		}
		p, ok := provider.(api.TaskFileReader)
		if !ok {
			return compactError("unavailable", errors.New("Worker files unavailable"))
		}
		id, ref := str(q, "task"), str(q, "file")
		v, err := p.ReadTaskFile(bounded, id, ref)
		if err != nil {
			return compactError("native_file_unavailable", err)
		}
		if native, ok := api.ParseTaskArtifactID(ref); ok {
			ref = native.ID
		}
		return compactValue(map[string]any{"ok": true, "data": map[string]any{"artifact": api.Artifact{ID: api.TaskArtifactID(id, ref), Name: v.Name}, "bytes": len(v.Data)}}, false)
	case "finish":
		p, ok := provider.(api.TaskFinisher)
		if !ok {
			return compactError("unavailable", errors.New("task finish unavailable"))
		}
		task, err := p.FinishTask(bounded, str(q, "task"), str(q, "token"))
		if err != nil {
			return compactError("task_finish_unconfirmed", err)
		}
		return compactValue(map[string]any{"ok": true, "data": taskReceipt(task)}, false)
	case "stop":
		task, err := provider.StopTask(bounded, str(q, "task"))
		if err != nil {
			return compactError("native_stop_unconfirmed", errors.New("original task stop is not confirmed; read the task before another action"))
		}
		return compactValue(map[string]any{"ok": true, "data": taskReceipt(task)}, false)
	case "pin":
		catalog, ok := provider.(api.TaskCatalog)
		if !ok {
			return compactError("unavailable", errors.New("task card pinning unavailable"))
		}
		pinned, _ := q["pinned"].(bool)
		item, err := catalog.PinTask(str(q, "task"), pinned)
		if err != nil {
			return compactError("pin_failed", err)
		}
		return compactValue(map[string]any{"ok": true, "data": map[string]any{"id": item.ID, "status": item.Status, "pinned": item.Pinned}}, false)
	}
	return compactError("invalid_arguments", errors.New("unsupported task operation"))
}

// Keep a short failure accessible even when large original messages are chunked.
// The chunked JSON retains the same fact as metadata, never an invented message.
func taskReadValue(data map[string]any, failure *api.TaskFailure, q map[string]any) api.ToolResult {
	value := map[string]any{"ok": true, "data": data}
	if failure != nil {
		data["failure"] = failure
		value["failure"] = failure
	}
	return fullTaskValue(value, q)
}
