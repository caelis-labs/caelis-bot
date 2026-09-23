package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (r *Runtime) ConfigurePersonal(store api.PersonalTools) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil || r.stopped {
		return errors.New("个人空间须在连接前绑定")
	}
	if store == nil {
		return errors.New("个人空间不可为空")
	}
	// A durable capability transition at connection assembly, not an incidental
	// note edit or context compaction. Unknown future versions fail closed on load.
	if r.state.PersonalVersion == 0 {
		r.state.PersonalVersion = 1
		if e := r.saveLocked(); e != nil {
			r.state.PersonalVersion = 0
			return e
		}
	}
	r.personal = store
	return nil
}
func strictArgs(raw json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("参数无效")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("参数无效")
	}
	return nil
}
func (r *Runtime) callPersonal(ctx context.Context, name string, args json.RawMessage) toolResult {
	r.mu.Lock()
	store, provider := r.personal, r.provider
	r.mu.Unlock()
	if store == nil {
		return result(nil, errors.New("个人空间暂不可用"))
	}
	if name != "bot_memory" {
		return result(nil, errors.New("不支持的 Bot 操作"))
	}
	var in struct {
		Operation string `json:"operation"`
		Query     string `json:"query"`
		api.MemoryChange
	}
	if e := strictArgs(args, &in); e != nil {
		return result(nil, e)
	}
	switch in.Operation {
	case "recall":
		v, e := store.ReadMemory(ctx, in.Query)
		return result(v, e)
	case "remember":
		v, e := store.Remember(ctx, in.RequestID, in.Text, provider)
		return result(v, e)
	case "correct":
		return result("线索已更正", store.CorrectMemory(ctx, in.MemoryChange))
	case "forget":
		return result("线索已遗忘", store.ForgetMemory(ctx, in.MemoryChange))
	}
	return result(nil, errors.New("不支持的记忆操作"))
}

func personalSpecs() []any {
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	return []any{map[string]any{"name": "bot_memory", "description": "Recall, remember, correct or forget Bot-private evidence shared across Runtime choices. Notebook Markdown is maintained directly with file tools; MEMORY.md is the sole core memory. Evidence is not permission or a confirmed profile. Stable requestId is required for mutations. Correct/forget target a receipt id returned by recall.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"operation"}, "properties": map[string]any{"operation": map[string]any{"type": "string", "enum": []string{"recall", "remember", "correct", "forget"}}, "query": str("Optional keywords"), "requestId": str("Stable operation id"), "id": str("Evidence receipt id"), "text": str("Evidence text")}}}}
}
