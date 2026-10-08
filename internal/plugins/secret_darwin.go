//go:build darwin && cgo

package plugins

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>
static CFMutableDictionaryRef plugin_secret_query(const char *account) {
 CFMutableDictionaryRef q=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef a=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(q,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(q,kSecAttrService,CFSTR("dev.caelis.bot.plugins"));
 CFDictionarySetValue(q,kSecAttrAccount,a);CFRelease(a);return q;
}
static OSStatus plugin_secret_save(const char *account,const char *secret) {
 CFMutableDictionaryRef q=plugin_secret_query(account);
 CFDataRef d=CFDataCreate(NULL,(const UInt8 *)secret,strlen(secret));
 const void *keys[]={kSecValueData};const void *values[]={d};
 CFDictionaryRef attrs=CFDictionaryCreate(NULL,keys,values,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 OSStatus s=SecItemUpdate(q,attrs);
 if(s==errSecItemNotFound){CFDictionarySetValue(q,kSecValueData,d);s=SecItemAdd(q,NULL);}
 CFRelease(attrs);CFRelease(d);CFRelease(q);return s;
}
static char *plugin_secret_load(const char *account) {
 CFMutableDictionaryRef q=plugin_secret_query(account);
 CFDictionarySetValue(q,kSecReturnData,kCFBooleanTrue);CFDictionarySetValue(q,kSecMatchLimit,kSecMatchLimitOne);
 CFTypeRef v=NULL;OSStatus s=SecItemCopyMatching(q,&v);CFRelease(q);
 if(s!=errSecSuccess||!v)return NULL;
 if(CFGetTypeID(v)!=CFDataGetTypeID()){CFRelease(v);return NULL;}
 CFIndex n=CFDataGetLength((CFDataRef)v);char *out=malloc(n+1);
 if(out){memcpy(out,CFDataGetBytePtr((CFDataRef)v),n);out[n]=0;}
 CFRelease(v);return out;
}
static OSStatus plugin_secret_delete(const char *account) {
 CFMutableDictionaryRef q=plugin_secret_query(account);OSStatus s=SecItemDelete(q);CFRelease(q);
 return s==errSecItemNotFound?errSecSuccess:s;
}
*/
import "C"
import (
	"errors"
	"unsafe"
)

const oauthNativeSupported = true

func saveSecret(id, secret string) error {
	a, b := C.CString(id), C.CString(secret)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	if C.plugin_secret_save(a, b) != 0 {
		return errors.New("keychain_save_failed")
	}
	return nil
}
func loadSecret(id string) (string, error) {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	p := C.plugin_secret_load(a)
	if p == nil {
		return "", errors.New("keychain_read_failed")
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), nil
}
func deleteSecret(id string) error {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	if C.plugin_secret_delete(a) != 0 {
		return errors.New("keychain_delete_failed")
	}
	return nil
}
