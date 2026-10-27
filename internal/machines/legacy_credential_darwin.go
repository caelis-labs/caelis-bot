//go:build darwin && cgo

package machines

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
static OSStatus delete_legacy_bot_ssh_credential(const char *account) {
 CFMutableDictionaryRef query=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef name=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(query,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(query,kSecAttrService,CFSTR("dev.caelis.bot.ssh"));
 CFDictionarySetValue(query,kSecAttrAccount,name);CFRelease(name);
 OSStatus status=SecItemDelete(query);CFRelease(query);
 return status==errSecItemNotFound?errSecSuccess:status;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// DeleteLegacyCredential removes only this v1 connection's Keychain account.
// It never reads a secret, accesses SSH key files, or reads the stored secret.
func DeleteLegacyCredential(id string) error {
	account := C.CString(id)
	defer C.free(unsafe.Pointer(account))
	if status := C.delete_legacy_bot_ssh_credential(account); status != 0 {
		return fmt.Errorf("machine_legacy_credential_cleanup (%d)", int(status))
	}
	return nil
}
