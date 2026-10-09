package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

type deliveryTraceKey struct{}

type deliveryTrace struct {
	emit        func(diagnosticlog.Record)
	fingerprint string
	part        int
}

func withDeliveryTrace(ctx context.Context, emit func(diagnosticlog.Record), key string, part int) context.Context {
	if emit == nil {
		return ctx
	}
	// The original delivery key stays in the private ledger. Diagnostics retain
	// only a stable fingerprint and part number for local correlation.
	return context.WithValue(ctx, deliveryTraceKey{}, deliveryTrace{emit: emit, fingerprint: digest(key), part: part})
}

func reportDeliveryAttempt(ctx context.Context, method, format string, err error, fallback bool) {
	trace, ok := ctx.Value(deliveryTraceKey{}).(deliveryTrace)
	if !ok || trace.emit == nil || err == nil && !fallback {
		return
	}
	code, category := 0, "sent"
	detail := ""
	if err != nil {
		category = "unknown"
		var transport *transportError
		if errors.As(err, &transport) {
			code = transport.code
			if transport.category != "" {
				category = transport.category
			}
			detail = transport.detail
		}
	}
	if category == "unchanged" {
		return
	}
	level := "info"
	if err != nil {
		level = "warning"
	}
	reason := fmt.Sprintf("api_code=%d category=%s", code, category)
	if detail != "" {
		reason += " detail=" + detail
	}
	trace.emit(diagnosticlog.Record{
		Level: level, Component: "telegram", Code: "delivery_attempt",
		Method: method, Phase: format, Fingerprint: trace.fingerprint,
		Sequence: uint64(trace.part + 1),
		Reason:   reason,
	})
}
