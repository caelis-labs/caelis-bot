#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-care.XXXXXX")
trap 'rm -rf "$directory"' EXIT
cat > "$directory/main.m" <<'OBJC'
#import <Cocoa/Cocoa.h>
#include "care_darwin.m"
#include <assert.h>
int main(void) { @autoreleasepool {
 [NSApplication sharedApplication];
 char *bytes=bot_care_sample(); assert(bytes);
 NSData *data=[NSData dataWithBytes:bytes length:strlen(bytes)];free(bytes);
 NSDictionary *sample=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];assert(sample);
 assert([sample[@"Awake"] isKindOfClass:NSNumber.class]);
 assert(sample[@"Unlocked"]==NSNull.null || [sample[@"Unlocked"] isKindOfClass:NSNumber.class]);
 assert([sample[@"IdleSeconds"] doubleValue]>=0);
 assert([sample[@"Application"] isKindOfClass:NSString.class]);
 printf("Native care metadata: awake=%s lock-known=%s unlocked=%s. No app identifiers or content recorded.\n",[sample[@"Awake"] boolValue]?"yes":"no",sample[@"Unlocked"]==NSNull.null?"no":"yes",sample[@"Unlocked"]!=NSNull.null&&[sample[@"Unlocked"] boolValue]?"yes":"no");
 } return 0; }
OBJC
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/desktop" -framework Cocoa -framework CoreGraphics -framework IOKit "$directory/main.m" -o "$directory/fixture"
"$directory/fixture"
