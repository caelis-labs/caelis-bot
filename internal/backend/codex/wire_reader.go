package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/wirejson"
)

func readProjectedWire(r *bufio.Reader, framed bool) (wireMessage, error) {
	read := wirejson.ReadLine
	if framed {
		read = wirejson.Read
	}
	data, header, err := read(r)
	if errors.Is(err, wirejson.ErrTooLarge) {
		return wireMessage{ID: header.ID, Method: header.Method}, ErrFrameTooLarge
	}
	if err != nil {
		if errors.Is(err, wirejson.ErrInvalid) {
			return wireMessage{}, ErrJSONDecode
		}
		return wireMessage{}, err
	}
	var msg wireMessage
	if json.Unmarshal(data, &msg) != nil {
		return wireMessage{}, ErrJSONDecode
	}
	return msg, nil
}
