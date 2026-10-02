package workerwire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// MaxRelayFrame bounds one closed Worker frame transported through an agent.
const MaxRelayFrame = 16 << 20

func ValidateRelayPair(pair Pair) error {
	knownBackend := func(value string) bool { return value == string(api.NodeCodex) || value == string(api.NodeCaelis) }
	if !pair.valid() || !knownBackend(pair.Target.Backend) || !knownBackend(pair.SourceBackend) {
		return errors.New("invalid native Worker pairing")
	}
	return nil
}

// ReadRelayFrame validates the same closed schema and pairing as the native
// Worker protocol. It does not admit work; admission remains with the owner.
func ReadRelayFrame(r io.Reader, pair Pair, request bool) ([]byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n == 0 || n > MaxRelayFrame {
		return nil, errors.New("Worker relay frame limit")
	}
	b := make([]byte, int(n)+4)
	copy(b, prefix[:])
	if _, err := io.ReadFull(r, b[4:]); err != nil {
		return nil, err
	}
	f, err := readFrame(bytes.NewReader(b))
	if err != nil || ValidateRelayPair(pair) != nil || f.Pair != pair || request && !validRequest(f) || !request && f.Method != "" {
		return nil, errors.New("Worker relay schema or pairing mismatch")
	}
	for _, source := range []*api.WorkDispatchSource{f.Source, f.Current} {
		if source != nil && (source.Validate() != nil || source.NodeID != pair.SourceNode || source.Backend != pair.SourceBackend) {
			return nil, errors.New("Worker relay source mismatch")
		}
		if source != nil && source.Lease != (api.WorkerLeaseGrant{}) && api.ProfileBotID(source.Lease.BotID) != pair.BotID {
			return nil, errors.New("Worker relay lease origin mismatch")
		}
	}
	if f.Start != nil && (f.Start.Target == nil || *f.Start.Target != pair.Target) || f.Approval != nil && f.Approval.Target != pair.Target {
		return nil, errors.New("Worker relay target mismatch")
	}
	return b, nil
}
