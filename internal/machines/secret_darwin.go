//go:build darwin && cgo

package machines

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>
static CFMutableDictionaryRef bot_secret_query(const char *account) {
 CFMutableDictionaryRef query=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef name=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(query,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(query,kSecAttrService,CFSTR("dev.caelis.bot.ssh"));
 CFDictionarySetValue(query,kSecAttrAccount,name);CFRelease(name);return query;
}
static OSStatus bot_secret_save(const char *account,const char *secret) {
 CFMutableDictionaryRef query=bot_secret_query(account);
 CFDataRef data=CFDataCreate(NULL,(const UInt8 *)secret,strlen(secret));
 const void *keys[]={kSecValueData};const void *values[]={data};
 CFDictionaryRef attributes=CFDictionaryCreate(NULL,keys,values,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 OSStatus status=SecItemUpdate(query,attributes);
 if(status==errSecItemNotFound){CFDictionarySetValue(query,kSecValueData,data);status=SecItemAdd(query,NULL);}
 CFRelease(attributes);CFRelease(data);CFRelease(query);return status;
}
static char *bot_secret_load(const char *account) {
 CFMutableDictionaryRef query=bot_secret_query(account);
 CFDictionarySetValue(query,kSecReturnData,kCFBooleanTrue);CFDictionarySetValue(query,kSecMatchLimit,kSecMatchLimitOne);
 CFTypeRef value=NULL;OSStatus status=SecItemCopyMatching(query,&value);CFRelease(query);
 if(status!=errSecSuccess||!value)return NULL;
 if(CFGetTypeID(value)!=CFDataGetTypeID()){CFRelease(value);return NULL;}
 CFIndex size=CFDataGetLength((CFDataRef)value);char *out=malloc(size+1);
 if(out){memcpy(out,CFDataGetBytePtr((CFDataRef)value),size);out[size]=0;}
 CFRelease(value);return out;
}
static OSStatus bot_secret_delete(const char *account) {
 CFMutableDictionaryRef query=bot_secret_query(account);OSStatus status=SecItemDelete(query);CFRelease(query);
 return status==errSecItemNotFound?errSecSuccess:status;
}
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
	if C.bot_secret_save(a, b) != 0 {
		return errors.New("keychain_save_failed")
	}
	return nil
}
func loadSecret(id string) (string, error) {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	p := C.bot_secret_load(a)
	if p == nil {
		return "", errors.New("keychain_read_failed")
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), nil
}
func deleteSecret(id string) error {
	a := C.CString(id)
	defer C.free(unsafe.Pointer(a))
	if C.bot_secret_delete(a) != 0 {
		return errors.New("keychain_delete_failed")
	}
	return nil
}
