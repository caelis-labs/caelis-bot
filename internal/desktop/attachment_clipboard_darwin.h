// Returns a malloc-owned JSON document containing native file URLs or one PNG.
// The caller frees the returned pointer.
char *bot_attachment_clipboard(void);
char *bot_attachment_clipboard_named(const char *name);
