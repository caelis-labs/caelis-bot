#!/usr/bin/env bash
# Exercises the actual pinned Sparkle ABI and our delegate on the AppKit thread.
set -euo pipefail
source "$(dirname "$0")/env.sh"
source script/sparkle.sh
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-sparkle-test.XXXXXX")
trap 'rm -rf "$directory" "$BOT_SPARKLE_DIR"' EXIT
bundle="$directory/Updater Test.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Frameworks"
ditto "$BOT_SPARKLE_DIR/Sparkle.framework" "$bundle/Contents/Frameworks/Sparkle.framework"
cp resources/macos/Info.plist "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set CFBundleExecutable fixture' "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set CFBundleIdentifier dev.caelis.bot.updater-test' "$bundle/Contents/Info.plist"
# Ephemeral fixture key, never persisted in Keychain or used for a release.
export BOT_SPARKLE_PUBLIC_KEY=$(node --input-type=module -e 'import {generateKeyPairSync} from "node:crypto";console.log(generateKeyPairSync("ed25519").publicKey.export({format:"der",type:"spki"}).subarray(-32).toString("base64"))')
BOT_RELEASE_TAG=v1.0.0 node script/configure-updates.mjs "$bundle/Contents/Info.plist"
cat > "$directory/main.m" <<'OBJC'
#import <Cocoa/Cocoa.h>
#include "updater_darwin.m"
#include <assert.h>
static int preparations;
void botUpdaterPrepare(void) { preparations++; }
int main(void) {
  @autoreleasepool {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    [NSUserDefaults.standardUserDefaults removePersistentDomainForName:NSBundle.mainBundle.bundleIdentifier];
    assert(bot_updater_start() == 1);
    assert(bot_updater_automatic());
    bot_updater_set_automatic(false);
    assert(!bot_updater_automatic());
    __block int installs = 0;
    assert([updateDelegate updater:nil shouldPostponeRelaunchForUpdate:nil untilInvokingBlock:^{ installs++; }]);
    assert(preparations == 1 && installs == 0 && bot_updater_waiting());
    [updateDelegate updater:nil didAbortWithError:nil];
    assert(!bot_updater_waiting() && !bot_updater_claim() && installs == 0);
    [updateDelegate updater:nil shouldPostponeRelaunchForUpdate:nil untilInvokingBlock:^{ installs++; }];
    assert(bot_updater_claim());
    bot_updater_finish();
    assert(installs == 1 && !bot_updater_waiting());
    bot_updater_stop();
    [NSUserDefaults.standardUserDefaults removePersistentDomainForName:NSBundle.mainBundle.bundleIdentifier];
    puts("Pinned Sparkle loaded; settings, postponed installation and cancellation passed.");
  }
  return 0;
}
OBJC
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/desktop" -framework Cocoa "$directory/main.m" -o "$bundle/Contents/MacOS/fixture"
bash script/sign-sparkle.sh "$bundle" -
codesign --force --sign - "$bundle"
"$bundle/Contents/MacOS/fixture"
