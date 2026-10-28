package sshtransport

import (
	"strings"
	"testing"
	"time"
)

func TestRetryBackoffCapsIntervalWithoutEndingRecovery(t *testing.T) {
	for _, failures := range []int{0, 1, 2, 3, 4, 5, 6, 32, 1000000} {
		for range 100 {
			delay := retryDelay(failures)
			if delay < 4*time.Second || delay > maxRetryDelay || (failures >= 6 && delay < maxRetryDelay*4/5) {
				t.Fatalf("failure count %d: delay %s", failures, delay)
			}
		}
	}
}

func TestReconnectClassifiesTransientAndUserActionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		stderr, code string
		retry        bool
	}{
		{"Connection refused", "ssh_host_unreachable", true},
		{"Operation timed out", "ssh_host_unreachable", true},
		{"Network is unreachable", "ssh_host_unreachable", true},
		{"Network is down", "ssh_host_unreachable", true},
		{"Host is down", "ssh_host_unreachable", true},
		{"Timeout, server private-host not responding.", "ssh_host_unreachable", true},
		{"Software caused connection abort", "ssh_disconnected", true},
		{"Could not resolve hostname private-host", "ssh_host_not_found", true},
		{"kex_exchange_identification: Connection closed by remote host", "ssh_disconnected", true},
		{"Connection reset by peer", "ssh_disconnected", true},
		{"Broken pipe", "ssh_disconnected", true},
		{"Permission denied (publickey,password)", "ssh_authentication_failed", false},
		{"REMOTE HOST IDENTIFICATION HAS CHANGED!", "ssh_host_key_changed", false},
		{"Host key verification failed.", "ssh_authentication_required", false},
		{"Bad configuration option: unknown", "ssh_config_syntax", false},
		{"unclassified private failure", "ssh_authentication_or_connection_failed", false},
	} {
		t.Run(tc.code+tc.stderr[:3], func(t *testing.T) {
			var d diagnostic
			d.Write([]byte(strings.Repeat("private", 2000)))
			d.Write([]byte(tc.stderr))
			if len(d.tail) > 8192 || d.code() != tc.code || d.tail != "" || retryable(tc.code) != tc.retry {
				t.Fatal("unbounded/unclassified diagnostic or unsafe retry policy")
			}
		})
	}
	for _, code := range []string{"ssh_stopped", "ssh_closed", "ssh_authentication_timeout", "ssh_config_unreadable", "ssh_key_required", "ssh_not_installed"} {
		if retryable(code) {
			t.Fatal("automatic retry crossed user-action boundary", code)
		}
	}
}
