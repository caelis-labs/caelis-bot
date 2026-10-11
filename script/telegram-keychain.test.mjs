import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawnSync} from 'node:child_process';

test('native channel token lookup is noninteractive without using LocalAuthentication or the real Keychain', {skip: process.platform !== 'darwin'}, () => {
  const directory = mkdtempSync(join(tmpdir(), 'bot-telegram-keychain-'));
  try {
    for (const provider of ['telegram', 'weixin']) {
      const source = join(directory, `${provider}-fixture.m`), binary = join(directory, `${provider}-fixture`);
      writeFileSync(source, `
#import <Foundation/Foundation.h>
#import <Security/Security.h>
#include <assert.h>
#include <string.h>
@interface FixtureAuthenticationContext : NSObject { BOOL _interactionNotAllowed; }
@property(nonatomic) BOOL interactionNotAllowed;
@end
@implementation FixtureAuthenticationContext
@synthesize interactionNotAllowed = _interactionNotAllowed;
@end
static OSStatus outcome = errSecSuccess;
static OSStatus fixture_copy(CFDictionaryRef query, CFTypeRef *value) {
  FixtureAuthenticationContext *context = (FixtureAuthenticationContext *)CFDictionaryGetValue(query, kSecUseAuthenticationContext);
  assert([context isKindOfClass:FixtureAuthenticationContext.class] && context.interactionNotAllowed);
  assert(CFEqual(CFDictionaryGetValue(query, kSecAttrService), CFSTR("dev.caelis.bot.${provider}")));
  assert(CFEqual(CFDictionaryGetValue(query, kSecAttrAccount), CFSTR("synthetic-account")));
  assert(CFDictionaryGetValue(query, kSecReturnData) == kCFBooleanTrue);
  *value = outcome == errSecSuccess ? CFDataCreate(NULL, (const UInt8 *)"synthetic-token", 15) : NULL;
  return outcome;
}
#define BOT_AUTHENTICATION_CONTEXT_CLASS FixtureAuthenticationContext
#define SecItemCopyMatching fixture_copy
#include "secret_darwin.m"
int main(void) {
  char *value = bot_${provider}_secret_load("synthetic-account");
  assert(value && !strcmp(value, "synthetic-token")); free(value);
  outcome = errSecInteractionNotAllowed; assert(!bot_${provider}_secret_load("synthetic-account"));
  outcome = errSecItemNotFound; assert(!bot_${provider}_secret_load("synthetic-account"));
  return 0;
}
`);
      const compiled = spawnSync('clang', ['-Werror=deprecated-declarations', '-mmacosx-version-min=12.0', '-I', resolve(`internal/${provider}`), '-framework', 'Foundation', '-framework', 'Security', source, '-o', binary], {encoding: 'utf8', timeout: 30000});
      assert.equal(compiled.status, 0, `${provider}: ${compiled.stderr}`);
      const result = spawnSync(binary, [], {encoding: 'utf8', timeout: 10000});
      assert.equal(result.status, 0, `${provider}: ${result.stderr}`);
    }
  } finally {rmSync(directory, {recursive: true, force: true});}
});
