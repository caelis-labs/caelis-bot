#import <Cocoa/Cocoa.h>
#import "attachment_clipboard_darwin.h"
#include <string.h>
#include <stdlib.h>

static char *attachmentPayload(NSPasteboard *board) {
    NSArray<NSURL *> *urls = [board readObjectsForClasses:@[NSURL.class]
                                                  options:@{NSPasteboardURLReadingFileURLsOnlyKey:@YES}];
    NSMutableArray<NSString *> *paths = NSMutableArray.array;
    for (NSURL *url in urls) {
        if (url.isFileURL && url.path.length) [paths addObject:url.path];
    }
    if (!paths.count) {
        id legacy = [board propertyListForType:NSFilenamesPboardType];
        if ([legacy isKindOfClass:NSArray.class]) {
            for (id value in legacy) if ([value isKindOfClass:NSString.class] && [value length]) [paths addObject:value];
        }
    }
    NSMutableDictionary *result = NSMutableDictionary.dictionary;
    if (paths.count) {
        // Finder may publish a preview image for the same copied file. The
        // file URLs are authoritative for this one paste operation.
        result[@"paths"] = paths;
    } else {
        NSData *png = [board dataForType:NSPasteboardTypePNG];
        BOOL imagePresent = png.length > 0 || [board.types containsObject:NSPasteboardTypeTIFF];
        if (!png.length) {
            NSImage *image = [[NSImage alloc] initWithPasteboard:board];
            imagePresent = imagePresent || image != nil;
            NSData *tiff = image.TIFFRepresentation;
            NSBitmapImageRep *bitmap = tiff ? [NSBitmapImageRep imageRepWithData:tiff] : nil;
            if (bitmap && bitmap.pixelsWide > 0 && bitmap.pixelsHigh > 0 &&
                bitmap.pixelsWide <= 16384 && bitmap.pixelsHigh <= 16384 &&
                (int64_t)bitmap.pixelsWide * bitmap.pixelsHigh <= 64000000) {
                png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
            }
        }
        if (png.length > 20 * 1024 * 1024) result[@"tooLarge"] = @YES;
        else if (png.length) result[@"image"] = [png base64EncodedStringWithOptions:0];
        else if (imagePresent) result[@"invalid"] = @YES;
    }
    NSData *json = [NSJSONSerialization dataWithJSONObject:result options:0 error:nil];
    if (!json) return NULL;
    char *text = malloc(json.length + 1);
    if (!text) return NULL;
    memcpy(text, json.bytes, json.length);
    text[json.length] = 0;
    return text;
}

char *bot_attachment_clipboard(void) {
    return attachmentPayload(NSPasteboard.generalPasteboard);
}

char *bot_attachment_clipboard_named(const char *name) {
    if (!name) return NULL;
    return attachmentPayload([NSPasteboard pasteboardWithName:[NSString stringWithUTF8String:name]]);
}
