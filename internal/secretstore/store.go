// Package secretstore defines only the credential operations consumed by Bot
// connections. Native adapters own Keychain or Credential Manager policy.
package secretstore

import "errors"

type Store interface {
	Save(id, secret string) error
	Load(id string) (string, error)
	Delete(id string) error
}

type Functions struct {
	SaveFunc   func(string, string) error
	LoadFunc   func(string) (string, error)
	DeleteFunc func(string) error
}

var ErrUnavailable = errors.New("secret store unavailable")

func (f Functions) Save(id, secret string) error {
	if f.SaveFunc == nil {
		return ErrUnavailable
	}
	return f.SaveFunc(id, secret)
}
func (f Functions) Load(id string) (string, error) {
	if f.LoadFunc == nil {
		return "", ErrUnavailable
	}
	return f.LoadFunc(id)
}
func (f Functions) Delete(id string) error {
	if f.DeleteFunc == nil {
		return ErrUnavailable
	}
	return f.DeleteFunc(id)
}
