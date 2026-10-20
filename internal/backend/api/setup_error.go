package api

// SetupBlockedError is a classified refusal before any setup effect. It may be
// returned in a Web receipt without exposing a native error or an OAuth payload.
type SetupBlockedError struct{ Code, Message string }

func (e *SetupBlockedError) Error() string { return e.Message }

// ActionRejectedError certifies that the owning boundary refused an action
// before dispatching any effect. It never classifies transport/write failures.
type ActionRejectedError struct{ Code string }

func (e *ActionRejectedError) Error() string { return e.Code }
