package caelisruntime

import (
	"context"
	"testing"
	"time"
)

func TestManageOperationDeadline(t *testing.T) {
	for _, action := range []string{"install", "update", "check-update", "start", "apply-update", "detect"} {
		ctx, cancel := manageContext(t.Context(), action)
		deadline, ok := ctx.Deadline()
		want := 5 * time.Minute
		if action == "install" {
			want = 15 * time.Minute
		}
		remaining := time.Until(deadline)
		if !ok || remaining > want || remaining < want-time.Second {
			t.Fatalf("%s: %v", action, remaining)
		}
		cancel()
	}
	parent, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	ctx, cancel := manageContext(parent, "install")
	defer cancel()
	actual, _ := ctx.Deadline()
	expected, _ := parent.Deadline()
	if !actual.Equal(expected) {
		t.Fatal("caller deadline was discarded")
	}
	stop()
	if ctx.Err() != context.Canceled {
		t.Fatal("caller cancellation was discarded")
	}
}
