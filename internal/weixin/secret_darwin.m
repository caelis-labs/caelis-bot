//go:build darwin && cgo
#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <Security/Security.h>
#include <stdlib.h>
#include <string.h>

#ifndef BOT_AUTHENTICATION_CONTEXT_CLASS
#define BOT_AUTHENTICATION_CONTEXT_CLASS LAContext
#endif

static CFMutableDictionaryRef bot_secret_query(const char *account) {
 CFMutableDictionaryRef query=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef name=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(query,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(query,kSecAttrService,CFSTR("dev.caelis.bot.weixin"));
 CFDictionarySetValue(query,kSecAttrAccount,name);CFRelease(name);return query;
}
int bot_weixin_secret_save(const char *account,const char *secret) {
 CFMutableDictionaryRef query=bot_secret_query(account);
 CFDataRef data=CFDataCreate(NULL,(const UInt8 *)secret,strlen(secret));
 const void *keys[]={kSecValueData};const void *values[]={data};
 CFDictionaryRef attributes=CFDictionaryCreate(NULL,keys,values,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 OSStatus status=SecItemUpdate(query,attributes);
 if(status==errSecItemNotFound){CFDictionarySetValue(query,kSecValueData,data);status=SecItemAdd(query,NULL);}
 CFRelease(attributes);CFRelease(data);CFRelease(query);return status;
}
char *bot_weixin_secret_load(const char *account) {
 @autoreleasepool {
  CFMutableDictionaryRef query=bot_secret_query(account);
  // Launch must never wait for a security dialog. A locked or changed keychain
  // grant is handled by the explicit Weixin connection status instead.
  BOT_AUTHENTICATION_CONTEXT_CLASS *context=[BOT_AUTHENTICATION_CONTEXT_CLASS new];
  context.interactionNotAllowed=YES;
  CFDictionarySetValue(query,kSecUseAuthenticationContext,(__bridge const void *)context);
  CFDictionarySetValue(query,kSecReturnData,kCFBooleanTrue);CFDictionarySetValue(query,kSecMatchLimit,kSecMatchLimitOne);
  CFTypeRef value=NULL;OSStatus status=SecItemCopyMatching(query,&value);CFRelease(query);[context release];
  if(status!=errSecSuccess||!value){if(value)CFRelease(value);return NULL;}
  if(CFGetTypeID(value)!=CFDataGetTypeID()){CFRelease(value);return NULL;}
  CFIndex size=CFDataGetLength((CFDataRef)value);char *out=malloc(size+1);
  if(out){memcpy(out,CFDataGetBytePtr((CFDataRef)value),size);out[size]=0;}
  CFRelease(value);return out;
 }
}
int bot_weixin_secret_delete(const char *account) {
 CFMutableDictionaryRef query=bot_secret_query(account);OSStatus status=SecItemDelete(query);CFRelease(query);
 return status==errSecItemNotFound?errSecSuccess:status;
}
