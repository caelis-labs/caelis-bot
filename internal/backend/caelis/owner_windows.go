package caelis

import "os"

// Windows needs a native ACL/credential-store adapter before activation.
func owned(os.FileInfo) bool { return false }
