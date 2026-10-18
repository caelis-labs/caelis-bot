package desktop

import (
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

func TestWindowsTaskThumbnailRetainsOnlyCurrentOwnedReceipt(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &windowsDriver{}
	s.started, s.native = true, d
	s.taskPreviews = []api.TaskPreview{{ID: "owned"}}
	s.taskWindowEvents = map[string]taskterminal.WindowEvent{"owned": {Revision: 1<<32 | 4}}
	s.storeWindowsTaskThumbnail("foreign", 1<<32|4, []byte("not an owned image"), nil)
	s.storeWindowsTaskThumbnail("owned", 1<<32|3, []byte("late image"), nil)
	if len(d.taskThumbnails) != 0 {
		t.Fatal("foreign or stale capture retained")
	}
	s.storeWindowsTaskThumbnail("owned", 1<<32|4, []byte("native fixture image"), nil)
	if s.WindowsTaskThumbnail("owned") == "" {
		t.Fatal("current receipt discarded")
	}
	s.taskWindowEvents["owned"] = taskterminal.WindowEvent{Revision: 2<<32 | 1}
	if s.WindowsTaskThumbnail("owned") != "" {
		t.Fatal("old binding image exposed")
	}
	s.storeWindowsTaskThumbnail("owned", 2<<32|1, []byte("new binding"), nil)
	entry := d.taskThumbnails["owned"]
	entry.Captured = time.Now().Add(-31 * time.Second)
	d.taskThumbnails["owned"] = entry
	if s.WindowsTaskThumbnail("owned") != "" {
		t.Fatal("expired image exposed")
	}
	d.pruneTaskThumbnails(nil)
	if len(d.taskThumbnails) != 0 {
		t.Fatal("removed task image retained")
	}
}
