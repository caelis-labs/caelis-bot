package desktop

import (
	"errors"
	"fmt"
)

type windowsHotkey struct {
	id       int
	shortcut Shortcut
}
type windowsHotkeys struct{ entries map[int]windowsHotkey }

func (h *windowsHotkeys) logical(native int) int {
	for logical, v := range h.entries {
		if v.id == native {
			return logical
		}
	}
	return 0
}

// Reserve a second native ID before releasing the old registration. A rejected
// edit cannot alter the previous key or the shared Registered state.
func (h *windowsHotkeys) replace(logical int, s Shortcut, register func(int, uint32, uint32) bool, unregister func(int) bool) error {
	old, exists := h.entries[logical]
	if exists && old.shortcut == s {
		return nil
	}
	if !s.Enabled {
		if exists && !unregister(old.id) {
			return errors.New("could not unregister shortcut")
		}
		delete(h.entries, logical)
		return nil
	}
	mods, key, err := windowsHotkeyCode(s)
	if err != nil {
		return err
	}
	next := 0x4B00 + logical*2
	if exists && old.id == next {
		next++
	}
	if !register(next, mods, key) {
		return errors.New("shortcut is already in use")
	}
	if exists && !unregister(old.id) {
		unregister(next)
		return errors.New("could not replace previous shortcut")
	}
	if h.entries == nil {
		h.entries = map[int]windowsHotkey{}
	}
	h.entries[logical] = windowsHotkey{next, s}
	return nil
}

func windowsHotkeyCode(s Shortcut) (uint32, uint32, error) {
	mods := uint32(0x4000)
	if s.Alt {
		mods |= 1
	}
	if s.Control {
		mods |= 2
	}
	if s.Shift {
		mods |= 4
	}
	if s.Meta {
		mods |= 8
	}
	var key uint32
	switch {
	case s.Key == "Space":
		key = 0x20
	case len(s.Key) == 4 && s.Key[:3] == "Key":
		key = uint32(s.Key[3])
	case len(s.Key) == 6 && s.Key[:5] == "Digit":
		key = uint32(s.Key[5])
	default:
		var f int
		if _, e := fmt.Sscanf(s.Key, "F%d", &f); e == nil && f >= 1 && f <= 12 {
			key = uint32(0x6f + f)
		}
	}
	if key == 0 {
		return 0, 0, errors.New("unsupported shortcut key")
	}
	return mods, key, nil
}
