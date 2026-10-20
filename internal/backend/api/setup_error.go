package api

// SetupBlockedError is a classified refusal before any setup effect. It may be
// returned in a Web receipt without exposing a native error or an OAuth payload.
type SetupBlockedError struct{ Code, Message string }

func (e *SetupBlockedError) Error() string { return e.Message }
