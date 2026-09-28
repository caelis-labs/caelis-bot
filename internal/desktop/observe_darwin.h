#include <stdint.h>
char *bot_observation_context(void *pointer);
// Runs off the AppKit main thread; bounded, no files or permission prompts.
char *bot_observation_image(uint32_t displayID, int width, int height);
