//go:build darwin && cgo && keychainfixture

package machines

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
static OSStatus create_legacy_test_credential(const char *account) {
 CFMutableDictionaryRef q=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef a=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDataRef d=CFDataCreate(NULL,(const UInt8 *)"fixture",7);
 CFDictionarySetValue(q,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(q,kSecAttrService,CFSTR("dev.caelis.bot.ssh"));
 CFDictionarySetValue(q,kSecAttrAccount,a);CFDictionarySetValue(q,kSecValueData,d);
 OSStatus s=SecItemAdd(q,NULL);CFRelease(q);CFRelease(a);CFRelease(d);return s;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

func saveLegacyCredentialFixture(id string) error {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	if status := C.create_legacy_test_credential(a); status != 0 {
		return fmt.Errorf("fixture keychain (%d)", int(status))
	}
	return nil
}
