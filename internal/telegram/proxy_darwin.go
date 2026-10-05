//go:build darwin && cgo

package telegram

/*
#cgo LDFLAGS: -framework CFNetwork -framework CoreFoundation
#include <CFNetwork/CFNetwork.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

typedef struct {
 atomic_bool cancelled;
 int done;
 CFArrayRef proxies;
} TelegramProxyTask;
static TelegramProxyTask *telegram_proxy_task(void) {
 TelegramProxyTask *task=calloc(1,sizeof(TelegramProxyTask));
 if(task)atomic_init(&task->cancelled,0);
 return task;
}
static void telegram_proxy_cancel(TelegramProxyTask *task) {
 atomic_store(&task->cancelled,1);
}
static CFURLRef telegram_proxy_url(const char *address) {
 CFStringRef value=CFStringCreateWithCString(NULL,address,kCFStringEncodingUTF8);
 if(!value)return NULL;
 CFURLRef url=CFURLCreateWithString(NULL,value,NULL);
 CFRelease(value);return url;
}
static void telegram_pac_result(void *info,CFArrayRef proxies,CFErrorRef error) {
 TelegramProxyTask *task=info;
 if(proxies&&!error)task->proxies=CFRetain(proxies);
 task->done=1;
 CFRunLoopStop(CFRunLoopGetCurrent());
}
static CFArrayRef telegram_pac(CFDictionaryRef proxy,CFURLRef target,TelegramProxyTask *task) {
 task->done=0;
 CFStringRef type=CFDictionaryGetValue(proxy,kCFProxyTypeKey);
 CFStreamClientContext context={0,task,NULL,NULL,NULL};
 CFRunLoopSourceRef source=NULL;
 if(CFEqual(type,kCFProxyTypeAutoConfigurationURL)) {
  CFURLRef url=CFDictionaryGetValue(proxy,kCFProxyAutoConfigurationURLKey);
  if(url)source=CFNetworkExecuteProxyAutoConfigurationURL(url,target,telegram_pac_result,&context);
 } else {
  CFStringRef script=CFDictionaryGetValue(proxy,kCFProxyAutoConfigurationJavaScriptKey);
  if(script)source=CFNetworkExecuteProxyAutoConfigurationScript(script,target,telegram_pac_result,&context);
 }
 if(!source)return NULL;
 CFStringRef mode=CFSTR("dev.caelis.bot.telegram.proxy");
 CFRunLoopRef loop=CFRunLoopGetCurrent();
 CFRunLoopAddSource(loop,source,mode);
 // A stalled discovery candidate must leave time to try the system's next
 // candidate. Request cancellation still stops the entire resolution.
 CFAbsoluteTime deadline=CFAbsoluteTimeGetCurrent()+3.0;
 while(!task->done&&!atomic_load(&task->cancelled)&&CFAbsoluteTimeGetCurrent()<deadline)CFRunLoopRunInMode(mode,0.05,0);
 CFRunLoopSourceInvalidate(source);
 CFRunLoopRemoveSource(loop,source,mode);
 CFRelease(source);
 CFArrayRef result=task->proxies;task->proxies=NULL;
 return result;
}
// Preserve preference order. Only an explicit None/DIRECT result means direct;
// failed PAC candidates fall through only to subsequent system candidates.
static char *telegram_proxy_list(CFArrayRef proxies,CFURLRef target,TelegramProxyTask *task,int depth,int *status) {
 if(!proxies||depth>1)return NULL;
 for(CFIndex index=0;index<CFArrayGetCount(proxies);index++) {
  if(atomic_load(&task->cancelled))return NULL;
  CFDictionaryRef p=CFArrayGetValueAtIndex(proxies,index);
  CFStringRef type=CFDictionaryGetValue(p,kCFProxyTypeKey);
  if(!type)continue;
  if(CFEqual(type,kCFProxyTypeNone)){*status=0;return NULL;}
  if(CFEqual(type,kCFProxyTypeAutoConfigurationURL)||CFEqual(type,kCFProxyTypeAutoConfigurationJavaScript)) {
   CFArrayRef resolved=telegram_pac(p,target,task);
   char *result=telegram_proxy_list(resolved,target,task,depth+1,status);
   if(resolved)CFRelease(resolved);
   if(*status==0)return result;
   continue;
  }
  const char *scheme=NULL;
  if(CFEqual(type,kCFProxyTypeHTTP)||CFEqual(type,kCFProxyTypeHTTPS))scheme="http";
  if(CFEqual(type,kCFProxyTypeSOCKS))scheme="socks5";
  if(!scheme)continue;
  CFStringRef host=CFDictionaryGetValue(p,kCFProxyHostNameKey);
  CFNumberRef port=CFDictionaryGetValue(p,kCFProxyPortNumberKey);
  char name[1024];int number=0;
  if(!host||!port||!CFStringGetCString(host,name,sizeof(name),kCFStringEncodingUTF8)||!name[0]||!CFNumberGetValue(port,kCFNumberIntType,&number)||number<=0||number>65535)return NULL;
  char *result=malloc(1100);
  if(!result)return NULL;
  if(strchr(name,':'))snprintf(result,1100,"%s://[%s]:%d",scheme,name,number);
  else snprintf(result,1100,"%s://%s:%d",scheme,name,number);
  *status=0;return result;
 }
 return NULL;
}
// Native fixtures supply explicit ordered candidates. Initialize CFNetwork's
// PAC machinery with CopyProxiesForURL, just as the production path does.
static CFArrayRef telegram_proxy_fixture(CFURLRef target,const char *pac,int fallback) {
 CFDictionaryRef settings=CFDictionaryCreate(NULL,NULL,NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFArrayRef initial=CFNetworkCopyProxiesForURL(target,settings);
 if(initial)CFRelease(initial);CFRelease(settings);
 CFURLRef url=telegram_proxy_url(pac);
 if(!url)return NULL;
 CFMutableArrayRef proxies=CFArrayCreateMutable(NULL,0,&kCFTypeArrayCallBacks);
 CFMutableDictionaryRef candidate=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFDictionarySetValue(candidate,kCFProxyTypeKey,kCFProxyTypeAutoConfigurationURL);
 CFDictionarySetValue(candidate,kCFProxyAutoConfigurationURLKey,url);
 CFArrayAppendValue(proxies,candidate);CFRelease(candidate);CFRelease(url);
 if(fallback) {
  const void *keys[]={kCFProxyTypeKey};const void *values[]={kCFProxyTypeNone};
  CFDictionaryRef direct=CFDictionaryCreate(NULL,keys,values,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
  CFArrayAppendValue(proxies,direct);CFRelease(direct);
 }
 return proxies;
}
// Read settings on demand, including exclusions. An optional PAC URL supplies
// isolated ordered candidates for native tests, without changing the Mac.
static char *telegram_system_proxy(const char *address,const char *pac,int fallback,TelegramProxyTask *task,int *status) {
 *status=1;
 CFURLRef url=telegram_proxy_url(address);
 if(!url)return NULL;
 CFArrayRef proxies=NULL;
 if(pac&&pac[0]) {
  proxies=telegram_proxy_fixture(url,pac,fallback);
 } else {
  CFDictionaryRef settings=CFNetworkCopySystemProxySettings();
  if(settings){proxies=CFNetworkCopyProxiesForURL(url,settings);CFRelease(settings);}
 }
 char *result=telegram_proxy_list(proxies,url,task,0,status);
 if(proxies)CFRelease(proxies);
 CFRelease(url);return result;
}
*/
import "C"
import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
	"unsafe"
)

// CFNetwork PAC run-loop events must be serialized. Acquisition and execution
// are both cancellable; one stalled PAC cannot strand subsequent requests.
var proxyResolution = make(chan struct{}, 1)

func systemProxy(req *http.Request) (*url.URL, error) {
	if value, err := http.ProxyFromEnvironment(req); value != nil || err != nil {
		return value, err
	}
	return resolveSystemProxy(req.Context(), req.URL.String(), "")
}

func resolveSystemProxy(ctx context.Context, address, pacURL string) (*url.URL, error) {
	return resolveProxyCandidates(ctx, address, pacURL, false)
}

// PAC/DIRECT overrides are isolated native candidate fixtures; empty PAC reads
// the current system configuration, preserving proxy exclusions and ordering.
func resolveProxyCandidates(ctx context.Context, address, pacURL string, fallback bool) (*url.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case proxyResolution <- struct{}{}:
		defer func() { <-proxyResolution }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	task := C.telegram_proxy_task()
	if task == nil {
		return nil, errors.New("proxy_unavailable")
	}
	defer C.free(unsafe.Pointer(task))
	cancelled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		C.telegram_proxy_cancel(task)
		close(cancelled)
	})
	defer func() {
		if !stop() {
			<-cancelled // Join the cancellation callback before freeing native state.
		}
	}()
	target, pac := C.CString(address), C.CString(pacURL)
	defer C.free(unsafe.Pointer(target))
	defer C.free(unsafe.Pointer(pac))
	var status C.int
	var direct C.int
	if fallback {
		direct = 1
	}
	value := C.telegram_system_proxy(target, pac, direct, task, &status)
	if value != nil {
		defer C.free(unsafe.Pointer(value))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, errors.New("proxy_unavailable")
	}
	if value == nil {
		return nil, nil
	}
	proxy, err := url.Parse(C.GoString(value))
	if err != nil || proxy.Hostname() == "" || proxy.User != nil || proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" {
		return nil, errors.New("proxy_unavailable")
	}
	return proxy, nil
}
