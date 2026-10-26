package textchannel

import (
	"crypto/sha256"
	"encoding/binary"
)

// This monotone, fixed-size privacy filter can only hide a retired quoted reply.
// It never supplies a native target, choice or authorization. A false positive
// hides extra text rather than exposing a former marked-secret answer.
const retiredCardPrivacyBytes = 32 * 1024

func privacyBits(key string) [4]uint32 {
	sum := sha256.Sum256([]byte(key))
	var bits [4]uint32
	for i := range bits {
		bits[i] = binary.LittleEndian.Uint32(sum[i*4:]) % (retiredCardPrivacyBytes * 8)
	}
	return bits
}
func (s *Store) retireSecretCard(key string) {
	if len(s.state.RetiredSecretCards) != retiredCardPrivacyBytes {
		s.state.RetiredSecretCards = make([]byte, retiredCardPrivacyBytes)
	}
	for _, bit := range privacyBits(key) {
		s.state.RetiredSecretCards[bit/8] |= 1 << (bit % 8)
	}
}
func (s *Store) retiredSecretCard(key string) bool {
	if len(s.state.RetiredSecretCards) != retiredCardPrivacyBytes {
		return false
	}
	for _, bit := range privacyBits(key) {
		if s.state.RetiredSecretCards[bit/8]&(1<<(bit%8)) == 0 {
			return false
		}
	}
	return true
}
