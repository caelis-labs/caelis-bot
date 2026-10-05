//go:build darwin && cgo

package telegram

/*
#cgo LDFLAGS: -framework CFNetwork -framework CoreFoundation
#include <CFNetwork/CFNetwork.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
// Read the system proxy on demand so a menu-bar launch does not depend on shell
// environment variables. CFNetwork also applies configured proxy exclusions.
static char *telegram_system_proxy(const char *address) {
 CFStringRef value=CFStringCreateWithCString(NULL,address,kCFStringEncodingUTF8);
 CFURLRef url=CFURLCreateWithString(NULL,value,NULL);CFRelease(value);
 CFDictionaryRef settings=CFNetworkCopySystemProxySettings();
 if(!url||!settings){if(url)CFRelease(url);if(settings)CFRelease(settings);return NULL;}
 CFArrayRef proxies=CFNetworkCopyProxiesForURL(url,settings);CFRelease(url);CFRelease(settings);
 char *result=NULL;
 if(proxies&&CFArrayGetCount(proxies)>0){
  CFDictionaryRef p=CFArrayGetValueAtIndex(proxies,0);
  CFStringRef type=CFDictionaryGetValue(p,kCFProxyTypeKey);
  const char *scheme=NULL;
  if(type&&(CFEqual(type,kCFProxyTypeHTTP)||CFEqual(type,kCFProxyTypeHTTPS)))scheme="http";
  if(type&&CFEqual(type,kCFProxyTypeSOCKS))scheme="socks5";
  CFStringRef host=CFDictionaryGetValue(p,kCFProxyHostNameKey);
  CFNumberRef port=CFDictionaryGetValue(p,kCFProxyPortNumberKey);
  char name[1024];int number=0;
  if(scheme&&host&&port&&CFStringGetCString(host,name,sizeof(name),kCFStringEncodingUTF8)&&CFNumberGetValue(port,kCFNumberIntType,&number)&&number>0&&number<=65535){
   result=malloc(1100);if(result)snprintf(result,1100,"%s://%s:%d",scheme,name,number);
  }
 }
 if(proxies)CFRelease(proxies);return result;
}
*/
import "C"
import (
	"net/http"
	"net/url"
	"unsafe"
)

func systemProxy(req *http.Request) (*url.URL, error) {
	if value, err := http.ProxyFromEnvironment(req); value != nil || err != nil {
		return value, err
	}
	address := C.CString(req.URL.String())
	defer C.free(unsafe.Pointer(address))
	value := C.telegram_system_proxy(address)
	if value == nil {
		return nil, nil
	}
	defer C.free(unsafe.Pointer(value))
	return url.Parse(C.GoString(value))
}
