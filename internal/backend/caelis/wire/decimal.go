package wire

import (
	"encoding/json"
	"errors"
	"strconv"
)

// Caelis 0.65.0 shared-native-worker command/operation receipts return an
// integer revision despite the public Uint64Decimal string schema. Accept that
// observed form losslessly; marshaling retains the schema's decimal string.
// Never route these counters through float64 (large revisions lose authority).
func (v *Uint64Decimal) UnmarshalJSON(raw []byte) error {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return errors.New("invalid revision")
		}
		// Preserve the existing zero value for omitted optional fixture/state
		// counters; this empty string is never a native revision precondition.
		if value == "" {
			*v = ""
			return nil
		}
	}
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return errors.New("invalid revision")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return errors.New("invalid revision")
		}
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return errors.New("invalid revision")
	}
	*v = Uint64Decimal(value)
	return nil
}
