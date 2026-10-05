//go:build darwin && cgo

package telegram

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation -framework LocalAuthentication -framework Foundation
#include <stdlib.h>
int bot_telegram_secret_save(const char *account,const char *secret);
char *bot_telegram_secret_load(const char *account);
int bot_telegram_secret_delete(const char *account);
*/
import "C"
import (
	"errors"
	"unsafe"
)

func saveSecret(id, secret string) error {
	a, b := C.CString(id), C.CString(secret)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	if C.bot_telegram_secret_save(a, b) != 0 {
		return errors.New("keychain_save_failed")
	}
	return nil
}
func loadSecret(id string) (string, error) {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	p := C.bot_telegram_secret_load(a)
	if p == nil {
		return "", errors.New("keychain_read_failed")
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), nil
}
func deleteSecret(id string) error {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	if C.bot_telegram_secret_delete(a) != 0 {
		return errors.New("keychain_delete_failed")
	}
	return nil
}
