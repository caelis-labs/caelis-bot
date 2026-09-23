package api

import "testing"

func TestWorkExecutionValidation(t *testing.T) {
	catalog := []ModelOption{{Model: "work", Efforts: []string{"low", "high"}, ServiceTiers: []ServiceTier{{ID: "priority"}}}}
	for _, v := range []WorkExecutionSettings{{}, {Model: "work", Effort: "high", ServiceTier: "priority"}} {
		if err := ValidateWorkExecution(v, catalog); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []WorkExecutionSettings{{Effort: "high"}, {ServiceTier: "priority"}, {Model: "missing", Effort: "high"}, {Model: "work"}, {Model: "work", Effort: "ultra"}, {Model: "work", Effort: "low", ServiceTier: "unknown"}} {
		if ValidateWorkExecution(v, catalog) == nil {
			t.Fatal("invalid work selection accepted", v)
		}
	}
}
