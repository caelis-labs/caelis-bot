package scheduledata

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPureConditionBoundsAndJSONNumbers(t *testing.T) {
	now := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	for _, n := range []any{float64(39233940530), json.Number("39233940530"), int64(39233940530)} {
		match, err := Test(context.Background(), `event.databaseId == 39233940530 && event.status == "completed" && local.hour == 16`, "Asia/Shanghai", map[string]any{"databaseId": n, "status": "completed"}, now)
		if err != nil || !match {
			t.Fatal(match, err)
		}
	}
	for _, expr := range []string{`event.`, `42`, `http.get("secret")`} {
		if Validate(expr, "UTC") == nil {
			t.Fatal("invalid expression accepted")
		}
	}
	if _, err := Test(context.Background(), "event.value > 0", "UTC", map[string]any{"value": json.Number("1e999")}, now); err == nil {
		t.Fatal("numeric overflow accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Test(ctx, "true", "UTC", nil, now); err == nil {
		t.Fatal("cancelled condition evaluated")
	}
}
