#import <Cocoa/Cocoa.h>
#import "../internal/desktop/attachment_clipboard_darwin.h"
#include <stdlib.h>

static NSDictionary *readPayload(NSPasteboard *board) {
    char *raw = bot_attachment_clipboard_named(board.name.UTF8String);
    if (!raw) { fprintf(stderr, "null payload from %s\n", board.name.UTF8String); return nil; }
    NSData *data = [[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding];
    free(raw);
    return [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
}

int main(void) { @autoreleasepool {
    [NSApplication sharedApplication];
    NSPasteboard *board = [NSPasteboard pasteboardWithUniqueName];
    NSString *root = [NSTemporaryDirectory() stringByAppendingPathComponent:NSUUID.UUID.UUIDString];
    [[NSFileManager defaultManager] createDirectoryAtPath:root withIntermediateDirectories:YES attributes:@{} error:nil];
    @try {
        [board clearContents];
        [board setString:@"普通文本" forType:NSPasteboardTypeString];
        if (readPayload(board).count != 0) return 1;

        NSURL *one = [NSURL fileURLWithPath:[root stringByAppendingPathComponent:@"one.txt"]];
        NSURL *two = [NSURL fileURLWithPath:[root stringByAppendingPathComponent:@"two.txt"]];
        [@"one" writeToURL:one atomically:YES encoding:NSUTF8StringEncoding error:nil];
        [@"two" writeToURL:two atomically:YES encoding:NSUTF8StringEncoding error:nil];
        NSImage *image = [[NSImage alloc] initWithSize:NSMakeSize(2, 2)];
        [image lockFocus]; [NSColor.redColor setFill]; NSRectFill(NSMakeRect(0, 0, 2, 2)); [image unlockFocus];

        [board clearContents];
        [board writeObjects:@[one, two, image]];
        NSDictionary *files = readPayload(board);
        if ([files[@"paths"] count] != 2 || files[@"image"] != nil ||
            ![files[@"paths"][0] isEqual:one.path] || ![files[@"paths"][1] isEqual:two.path]) {
            fprintf(stderr, "file payload: %s\n", files.description.UTF8String);
            return 2;
        }

        [board clearContents];
        [board setPropertyList:@[one.path, two.path] forType:@"NSFilenamesPboardType"];
        if ([readPayload(board)[@"paths"] count] != 2) return 4;

        [board clearContents];
        [board writeObjects:@[image]];
        NSDictionary *pasted = readPayload(board);
        NSData *png = [[NSData alloc] initWithBase64EncodedString:pasted[@"image"] options:0];
        if (png.length < 8 || memcmp(png.bytes, "\x89PNG\r\n\x1a\n", 8) != 0 || pasted[@"paths"] != nil) return 3;
        puts("ATTACHMENT CLIPBOARD NATIVE PASS: text, Finder file URLs, legacy file lists, mixed representations, image bytes");
        return 0;
    } @finally {
        [board releaseGlobally];
        [[NSFileManager defaultManager] removeItemAtPath:root error:nil];
    }
} }
