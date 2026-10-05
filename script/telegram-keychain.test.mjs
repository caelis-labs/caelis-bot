import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawnSync} from 'node:child_process';

test('native token lookup prohibits authentication UI and handles unavailable grants without using the real Keychain', {skip: process.platform !== 'darwin'}, () => {
  const directory = mkdtempSync(join(tmpdir(), 'bot-telegram-keychain-'));
  try {
    const source = join(directory, 'fixture.m'), binary = join(directory, 'fixture');
    writeFileSync(source, `
#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <Security/Security.h>
#include <assert.h>
#include <string.h>
static OSStatus outcome = errSecSuccess;
static OSStatus fixture_copy(CFDictionaryRef query, CFTypeRef *value) {
  LAContext *context = (__bridge LAContext *)CFDictionaryGetValue(query, kSecUseAuthenticationContext);
  assert([context isKindOfClass:LAContext.class] && context.interactionNotAllowed);
  assert(CFEqual(CFDictionaryGetValue(query, kSecAttrService), CFSTR("dev.caelis.bot.telegram")));
  assert(CFEqual(CFDictionaryGetValue(query, kSecAttrAccount), CFSTR("synthetic-account")));
  assert(CFDictionaryGetValue(query, kSecReturnData) == kCFBooleanTrue);
  *value = outcome == errSecSuccess ? CFDataCreate(NULL, (const UInt8 *)"synthetic-token", 15) : NULL;
  return outcome;
}
#define SecItemCopyMatching fixture_copy
#include "secret_darwin.m"
int main(void) {
  char *value = bot_telegram_secret_load("synthetic-account");
  assert(value && !strcmp(value, "synthetic-token")); free(value);
  outcome = errSecInteractionNotAllowed; assert(!bot_telegram_secret_load("synthetic-account"));
  outcome = errSecItemNotFound; assert(!bot_telegram_secret_load("synthetic-account"));
  return 0;
}
`);
    const compiled = spawnSync('clang', ['-Werror=deprecated-declarations', '-mmacosx-version-min=12.0', '-I', resolve('internal/telegram'), '-framework', 'Foundation', '-framework', 'LocalAuthentication', '-framework', 'Security', source, '-o', binary], {encoding: 'utf8', timeout: 30000});
    assert.equal(compiled.status, 0, compiled.stderr);
    const result = spawnSync(binary, [], {encoding: 'utf8', timeout: 10000});
    assert.equal(result.status, 0, result.stderr);
  } finally {rmSync(directory, {recursive: true, force: true});}
});
