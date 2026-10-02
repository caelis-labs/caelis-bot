package runtimemanagement

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

// RunCommand is a thin private native seam. Its trusted owner supplies Directory;
// strict requests contain no store/token/API-key/model-configuration fields.
func RunCommand(ctx context.Context, directory string, input io.Reader, output io.Writer) error {
	var request Request
	decoder := json.NewDecoder(io.LimitReader(input, 64<<10+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		_ = json.NewEncoder(output).Encode(Status{Outcome: "rejected", Message: "Invalid runtime management request"})
		return errors.New("invalid runtime management request")
	}
	manager, err := New(directory)
	if err != nil {
		_ = json.NewEncoder(output).Encode(Status{Outcome: "rejected", Message: err.Error()})
		return err
	}
	status, err := manager.Manage(ctx, request)
	if encodeErr := json.NewEncoder(output).Encode(status); encodeErr != nil {
		return errors.New("runtime management response unavailable")
	}
	return err
}
