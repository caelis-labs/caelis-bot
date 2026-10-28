//go:build workerpeere2e

package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const controlMarker = "WORKER_PEER_FIXTURE "

type fixtureFile struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type fixtureRequest struct {
	Action      string           `json:"action"`
	Environment string           `json:"environment,omitempty"`
	Task        string           `json:"task,omitempty"`
	Scenario    string           `json:"scenario,omitempty"`
	Title       string           `json:"title,omitempty"`
	Prompt      string           `json:"prompt,omitempty"`
	Wait        bool             `json:"wait,omitempty"`
	Steps       []string         `json:"steps,omitempty"`
	Attachments []map[string]any `json:"attachments,omitempty"`
	Operation   string           `json:"operation,omitempty"`
	Expected    []fixtureFile    `json:"expected,omitempty"`
}

func parseFixtureRequest(text string) (fixtureRequest, bool, error) {
	index := strings.LastIndex(text, controlMarker)
	if index < 0 {
		return fixtureRequest{}, false, nil
	}
	var request fixtureRequest
	err := json.NewDecoder(strings.NewReader(text[index+len(controlMarker):])).Decode(&request)
	return request, true, err
}
func (s *nativeServer) runTurn(thread, turn string, input []nativeInput, resident bool) {
	s.emit(thread, "turn/started", map[string]any{"threadId": thread, "turn": nativeTurn{ID: turn, Status: "inProgress"}})
	s.mu.Lock()
	t := s.state.Threads[thread]
	if !s.activeLocked(thread, turn) {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.turnCancel[turn] = cancel
	defer func() { cancel(); s.mu.Lock(); delete(s.turnCancel, turn); s.mu.Unlock() }()
	user := copyJSON(t.Turns[len(t.Turns)-1].Items[0])
	s.mu.Unlock()
	s.emit(thread, "item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": user})
	request, controlled, err := parseFixtureRequest(inputText(input))
	if err != nil {
		s.completeTurn(thread, turn, "failed", "Invalid synthetic fixture instruction")
		return
	}
	if resident && !controlled {
		s.completeTurn(thread, turn, "completed", "Synthetic resident ready. No account or model was used.")
		return
	}
	if resident && (request.Action == "delegate" || request.Action == "send") && len(request.Attachments) == 0 {
		text := inputText(input)
		if start := strings.Index(text, "<bot_uploaded_files>\n"); start >= 0 {
			var metadata []struct {
				Attachment map[string]any `json:"attachment"`
			}
			data := strings.SplitN(text[start+len("<bot_uploaded_files>\n"):], "\n</bot_uploaded_files>", 2)[0]
			if json.Unmarshal([]byte(data), &metadata) != nil {
				s.completeTurn(thread, turn, "failed", "invalid real upload references")
				return
			}
			for _, file := range metadata {
				request.Attachments = append(request.Attachments, file.Attachment)
			}
		}
	}
	if len(request.Expected) > 0 {
		if err = s.verifyNativeFiles(thread, turn, input, request.Expected); err != nil {
			s.completeTurn(thread, turn, "failed", err.Error())
			return
		}
	}
	if resident {
		err = s.residentTurn(ctx, thread, turn, request)
	} else {
		err = s.workerTurn(thread, turn, request)
	}
	if err != nil {
		s.completeTurn(thread, turn, "failed", "Synthetic native flow failed: "+err.Error())
	}
}

func (s *nativeServer) workerTurn(thread, turn string, request fixtureRequest) error {
	if request.Scenario == "async-question" {
		item := nativeItem{ID: "original-async-" + turn, Type: "agentMessage", Delivery: "async", Status: "completed"}
		item.Questions = append(item.Questions, struct {
			Title   string   `json:"title"`
			Options []string `json:"options"`
		}{Title: "隔离验收：续跑后的新问题", Options: []string{"保留原任务继续", "暂停等待"}})
		s.item(thread, turn, item, true)
		return nil // Native turn stays active while the product observes the question.
	}
	if request.Scenario == "hold" {
		s.item(thread, turn, nativeItem{ID: "partial-" + turn, Type: "agentMessage", Text: "Synthetic Worker is running and awaiting interruption.", Status: "inProgress"}, false)
		return nil
	}
	steps := request.Steps
	if len(steps) == 0 && request.Scenario != "" && request.Scenario != "csv" {
		steps = []string{request.Scenario}
	}
	for _, step := range steps {
		if err := s.elicit(thread, turn, step); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if !s.activeLocked(thread, turn) {
		s.mu.Unlock()
		return nil
	}
	cwd := s.state.Threads[thread].Cwd
	if !filepath.IsAbs(cwd) {
		s.mu.Unlock()
		return errors.New("native Worker workspace is not absolute")
	}
	path := filepath.Join(cwd, "fixture-result.csv")
	if err := os.WriteFile(path, []byte("name,value\nfixture,42\n"), 0600); err != nil {
		s.mu.Unlock()
		return err
	}
	s.state.Effects["csv:"+thread+":"+turn]++
	_ = s.saveLocked()
	s.mu.Unlock()
	text := fmt.Sprintf("Synthetic Worker completed. [fixture-result.csv](%s)", path)
	s.item(thread, turn, nativeItem{ID: "answer-" + turn, Type: "agentMessage", Text: text, Status: "inProgress"}, false)
	s.emit(thread, "item/agentMessage/delta", map[string]any{"threadId": thread, "turnId": turn, "itemId": "answer-" + turn, "delta": "Synthetic Worker completed. "})
	s.completeTurn(thread, turn, "completed", text)
	return nil
}

func (s *nativeServer) residentTurn(parent context.Context, thread, turn string, request fixtureRequest) error {
	s.mu.Lock()
	config, ok := s.services[thread]
	cwd := s.state.Threads[thread].Cwd
	if request.Task == "" {
		request.Task = s.state.Threads[thread].LastTask
	}
	s.mu.Unlock()
	if !ok {
		return errors.New("resident received no real Bot MCP service")
	}
	c, err := startBotMCP(parent, config, cwd)
	if err != nil {
		return err
	}
	defer c.close()
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	if _, err := c.request("fixture-tools-list-"+turn, "tools/list", map[string]any{}); err != nil {
		return err
	}
	if request.Action == "reconcile" {
		result, err := c.request("fixture-original-reconcile-"+turn, "tools/call", map[string]any{"name": "bot_tasks", "arguments": map[string]any{"request": map[string]any{"type": "reconcile", "task": request.Task, "requestId": request.Operation}}})
		if err != nil {
			return err
		}
		structured, _ := result["structuredContent"].(map[string]any)
		if structured["ok"] != true {
			detail, _ := structured["error"].(map[string]any)
			if detail["code"] != "original_operation_unavailable" {
				return errors.New("unexpected original reconciliation failure")
			}
			s.completeTurn(thread, turn, "completed", "Original operation unavailable; result remains unknown ("+request.Operation+")")
			return nil
		}
		data, _ := structured["data"].(map[string]any)
		s.completeTurn(thread, turn, "completed", fmt.Sprintf("Original operation reconciled: %v (%s)", data["outcome"], request.Operation))
		return nil
	}

	if request.Action == "finish" {
		observed, err := s.callBot(ctx, c, thread, turn, "finish-observe", "bot_tasks", map[string]any{"request": map[string]any{"type": "observe", "task": request.Task}})
		if err != nil {
			return err
		}
		data, _ := observed["data"].(map[string]any)
		_, err = s.callBot(ctx, c, thread, turn, "finish-original", "bot_tasks", map[string]any{"request": map[string]any{"type": "finish", "task": request.Task, "token": data["finishToken"]}})
		if err == nil {
			s.completeTurn(thread, turn, "completed", "Synthetic resident finished the inspected execution.")
		}
		return err
	}
	if request.Action == "present-question" {
		listed, err := s.callBot(ctx, c, thread, turn, "questions", "bot_interactions", map[string]any{"request": map[string]any{"type": "list"}})
		if err != nil {
			return err
		}
		data, _ := listed["data"].(map[string]any)
		rows, _ := data["questions"].([]any)
		for _, row := range rows {
			q, _ := row.(map[string]any)
			if q["id"] != "" {
				_, err = s.callBot(ctx, c, thread, turn, "present-original", "bot_interactions", map[string]any{"request": map[string]any{"type": "present", "question": q["id"]}})
				if err != nil {
					return err
				}
			}
		}
		s.completeTurn(thread, turn, "completed", "Synthetic resident presented the original pending question.")
		return nil
	}

	if request.Action == "stop" {
		_, err = s.callBot(ctx, c, thread, turn, "stop", "bot_tasks", map[string]any{"request": map[string]any{"type": "stop", "task": request.Task}})
		if err == nil {
			s.completeTurn(thread, turn, "completed", "Synthetic resident requested the original task's stop through bot_tasks.")
		}
		return err
	}
	if request.Action == "delegate" || request.Action == "send" {
		args := map[string]any{}
		if request.Action == "delegate" {
			environments, err := s.callBot(ctx, c, thread, turn, "environments", "bot_tasks", map[string]any{"request": map[string]any{"type": "environments"}})
			if err != nil {
				return err
			}
			target, err := selectEnvironment(environments["data"], request.Environment)
			if err != nil {
				return err
			}
			args["environment"] = target
			if request.Title != "" {
				args["title"] = request.Title
			}
		} else {
			args["task"] = request.Task
		}
		worker := request
		worker.Action, worker.Task, worker.Environment, worker.Wait = "worker", "", "", false
		args["prompt"] = controlMarker + string(raw(worker))
		if len(request.Attachments) > 0 {
			args["attachments"] = request.Attachments
		}
		delegated, err := s.callBot(ctx, c, thread, turn, "delegate", "bot_delegate", args)
		if err != nil {
			// An actual native admission may be uncertain while the original
			// Worker is already asking a question. Model the documented Agent
			// contract: query that exact receipt, never repeat bot_delegate.
			detail, _ := delegated["error"].(map[string]any)
			original, _ := delegated["data"].(map[string]any)
			taskID, _ := original["id"].(string)
			operation, _ := original["requestId"].(string)
			if detail["code"] != "unconfirmed" || taskID == "" || operation == "" {
				return err
			}
			if err := s.reconcileFixtureAdmission(ctx, c, thread, turn, taskID, operation); err != nil {
				return err
			}
		}
		data, _ := delegated["data"].(map[string]any)
		request.Task, _ = data["id"].(string)
		if request.Task == "" {
			return errors.New("real delegation returned no task")
		}
		s.mu.Lock()
		s.state.Threads[thread].LastTask = request.Task
		_ = s.saveLocked()
		s.mu.Unlock()
		if !request.Wait {
			s.completeTurn(thread, turn, "completed", "Synthetic resident delegated through real Bot MCP: "+request.Task)
			return nil
		}
	}
	if request.Task == "" {
		return errors.New("synthetic read requires an original task")
	}
	for attempt := range 100 {
		read, err := s.callBot(ctx, c, thread, turn, fmt.Sprintf("observe-%d", attempt), "bot_tasks", map[string]any{"request": map[string]any{"type": "observe", "task": request.Task}})
		if err != nil {
			return err
		}
		data, _ := read["data"].(map[string]any)
		task, _ := data["task"].(map[string]any)
		if task["status"] == "completed" {
			read, err = s.callBot(ctx, c, thread, turn, "completed-history", "bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": request.Task, "limit": 10}})
			if err != nil {
				return err
			}
			data, _ = read["data"].(map[string]any)
			file := firstArtifact(data["messages"])
			if file == "" {
				return errors.New("original Worker history contained no CSV artifact")
			}
			materialized, err := s.callBot(ctx, c, thread, turn, "file", "bot_tasks", map[string]any{"request": map[string]any{"type": "file", "task": request.Task, "file": file}})
			if err != nil {
				return err
			}
			copy, _ := materialized["data"].(map[string]any)
			path, _ := copy["localPath"].(string)
			if !filepath.IsAbs(path) {
				return errors.New("real Bot file tool did not return a checked A-local copy")
			}
			bytes, err := os.ReadFile(path) // Native file read after the actual tool materialized it.
			if err != nil {
				return err
			}
			rows, err := csv.NewReader(strings.NewReader(string(bytes))).ReadAll()
			if err != nil || len(rows) != 2 || len(rows[1]) != 2 || rows[1][0] != "fixture" || rows[1][1] != "42" {
				return errors.New("A-native read did not verify original CSV bytes")
			}
			sum := sha256.Sum256(bytes)
			digest := hex.EncodeToString(sum[:])
			if copy["sha256"] != digest {
				return errors.New("A-local copy digest does not match real file tool")
			}
			s.mu.Lock()
			s.state.Effects["verified-file:"+request.Task]++
			_ = s.saveLocked()
			s.mu.Unlock()
			s.completeTurn(thread, turn, "completed", fmt.Sprintf("Synthetic resident verified task %s through real bot_tasks read/file and an A-native file read: fixture-result.csv, %d bytes, SHA256 %s, row fixture=42.", request.Task, len(bytes), digest))
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("original synthetic Worker remained incomplete")
}

func selectEnvironment(data any, want string) (string, error) {
	rows, _ := data.([]any)
	for _, row := range rows {
		env, _ := row.(map[string]any)
		key, _ := env["key"].(string)
		if env["status"] == "connected" && (key == want || env["name"] == want || want == "" && key != "local") {
			return key, nil
		}
	}
	return "", errors.New("requested real execution environment is not connected")
}
func firstArtifact(value any) string {
	rows, _ := value.([]any)
	for _, row := range rows {
		message, _ := row.(map[string]any)
		artifacts, _ := message["artifacts"].([]any)
		for _, entry := range artifacts {
			artifact, _ := entry.(map[string]any)
			if id, _ := artifact["id"].(string); id != "" {
				return id
			}
		}
	}
	return ""
}

var documentFile = regexp.MustCompile(`\[用户附件：([^\n]+)\]\n本机文件副本路径：("(?:[^"\\]|\\.)*")`)

func (s *nativeServer) verifyNativeFiles(thread, turn string, input []nativeInput, expected []fixtureFile) error {
	files := map[string]string{}
	for _, part := range input {
		if part.Type == "localImage" {
			name := filepath.Base(part.Path)
			if _, rest, ok := strings.Cut(name, "-"); ok {
				name = rest
			}
			files[name] = part.Path
		}
		for _, match := range documentFile.FindAllStringSubmatch(part.Text, -1) {
			path, err := strconv.Unquote(match[2])
			if err != nil {
				return err
			}
			files[match[1]] = path
		}
	}
	for _, want := range expected {
		path := files[want.Name]
		rel, err := filepath.Rel(s.cwd, path)
		if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(rel) {
			return errors.New("native input has no scoped attachment: " + want.Name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if len(data) != want.Bytes || hex.EncodeToString(sum[:]) != want.SHA256 {
			return errors.New("native input bytes/name/digest mismatch: " + want.Name)
		}
		s.mu.Lock()
		s.state.Effects["verified-input:"+thread+":"+turn+":"+want.Name]++
		_ = s.saveLocked()
		s.mu.Unlock()
	}
	return nil
}

// Only the test resident uses this read-only original-receipt loop. It is not
// execution/approval state and cannot submit a replacement input.
func (s *nativeServer) reconcileFixtureAdmission(ctx context.Context, c *botMCP, thread, turn, taskID, operation string) error {
	for attempt := 0; attempt < 50; attempt++ {
		checked, readErr := s.callBot(ctx, c, thread, turn, fmt.Sprintf("original-admission-%d", attempt), "bot_tasks", map[string]any{"request": map[string]any{"type": "reconcile", "task": taskID, "requestId": operation}})
		if readErr != nil {
			return readErr
		}
		receipt, _ := checked["data"].(map[string]any)
		if receipt["outcome"] == "accepted" {
			return nil
		}
		if receipt["outcome"] != "unknown" {
			return fmt.Errorf("original admission refused: %v", receipt["outcome"])
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return errors.New("original admission remains unknown; no replacement or replay")
}
