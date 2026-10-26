package api

// TaskFailure is an immutable, privacy-safe projection of a native failed turn.
// It is not a retry authorization. Nil retry facts mean the Runtime did not
// supply them; Retryable concerns a model request, never replaying tool effects.
// Adapters must use typed native codes and fixed messages, not error prose.
type TaskFailure struct {
	Source         string `json:"source"`
	Execution      string `json:"execution"`
	Code           string `json:"code"`
	Reason         string `json:"reason"`
	Retryable      *bool  `json:"retryable"`
	WillRetry      *bool  `json:"willRetry"`
	RetryExhausted *bool  `json:"retryExhausted"`
}

func UnknownTaskFailure(source, execution string) *TaskFailure {
	return &TaskFailure{Source: source, Execution: execution, Code: "unknown", Reason: "The native turn failed; no reliable error category is available."}
}
