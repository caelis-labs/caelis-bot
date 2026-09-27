package taskterminal

import "errors"

// ErrLaunchNotSubmitted is evidence that no native launch/document request was
// submitted. Only this outcome allows retry after a failure without an owner.
var ErrLaunchNotSubmitted = errors.New("terminal launch was not submitted")

type launchNotSubmitted struct{ cause error }

func (e *launchNotSubmitted) Error() string        { return e.cause.Error() }
func (e *launchNotSubmitted) Unwrap() error        { return e.cause }
func (e *launchNotSubmitted) Is(target error) bool { return target == ErrLaunchNotSubmitted }

// NotLaunched preserves the cause while recording a proven pre-submission
// failure. Never use it for an unknown reply, timeout, or cancellation after
// dispatch; those must retain their native owner or prevent another launch.
func NotLaunched(err error) error {
	if err == nil {
		return nil
	}
	return &launchNotSubmitted{cause: err}
}
