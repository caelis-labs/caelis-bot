package machines

import "os"

func controlDirectoryRoot() string { return os.TempDir() }
