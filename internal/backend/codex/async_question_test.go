package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAsyncAgentMessageMetadataSurvivesNativeAndHostSnapshots(t *testing.T) {
	var item nativeItem
	if err := json.Unmarshal([]byte(`{"id":"call_17","type":"agentMessage","delivery":"async","text":"选择","questions":[{"title":"选哪项？","options":["甲","乙"]}]}`), &item); err != nil {
		t.Fatal(err)
	}
	s := NewSession(SessionOptions{Directory: t.TempDir(), StateFile: filepath.Join(t.TempDir(), "binding.json")})
	s.binding.ThreadID = "native-thread"
	s.applyItem("native-turn", item, true)
	s.publishSnapshot()
	snapshot := s.Snapshot()
	if snapshot.RuntimeOwner == "" || len(snapshot.Items) != 1 || snapshot.Items[0].AsyncCallID != "call_17" || len(snapshot.Items[0].AsyncQuestions) != 1 || snapshot.Items[0].AsyncQuestions[0].Options[1] != "乙" {
		t.Fatalf("async question metadata lost: %+v", snapshot)
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || json.Valid(b) == false || containsPrivateAsyncFields(string(b)) {
		t.Fatalf("host-only async metadata escaped renderer: %s", b)
	}
}

func containsPrivateAsyncFields(value string) bool {
	return strings.Contains(value, "asyncCallID") || strings.Contains(value, "asyncQuestions") || strings.Contains(value, "runtimeOwner")
}
