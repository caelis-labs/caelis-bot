package api

import "context"

type workerQuestionAnswerKey struct{}

// WithWorkerQuestionAnswer carries the host-validated original async question
// call through local admission. It is never model text or a tool argument.
// A peer already authenticates its originating host and original task binding.
func WithWorkerQuestionAnswer(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, workerQuestionAnswerKey{}, callID)
}
func WorkerQuestionAnswer(ctx context.Context) string {
	value, _ := ctx.Value(workerQuestionAnswerKey{}).(string)
	return value
}
