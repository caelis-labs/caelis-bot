package api

import (
	"encoding/json"
	"testing"
)

func TestDecisionValidationErrorHasStructuredUnsentCause(t *testing.T) {
	value := DecisionValidationError{Message: "private validation detail"}
	for _, err := range []error{value, &value} {
		data, e := json.Marshal(err)
		if e != nil || string(data) != `{"code":"decision_not_dispatched"}` {
			t.Fatalf("unsafe renderer cause: %s %v", data, e)
		}
	}
}
