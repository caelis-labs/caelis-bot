package codex

import "errors"

// ErrOwnedRuntimeUnsupported means this build cannot prove the independent
// native power and process fences required for an owned runtime. Refusal must
// precede starting helpers or creating runtime state.
var ErrOwnedRuntimeUnsupported = errors.New("independent owned runtime supervision unsupported by this build")
