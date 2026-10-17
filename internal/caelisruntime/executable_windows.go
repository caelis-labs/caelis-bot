package caelisruntime

import (
	"debug/pe"
	"os"
	"path/filepath"
	"strings"
)

// The selected runtime must be a native executable, not a command shell wrapper.
// Its version and public handshake still establish runtime compatibility.
func executableFile(path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return false
	}
	binary, err := pe.Open(path)
	if err != nil {
		return false
	}
	defer binary.Close()
	return binary.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 && binary.Characteristics&pe.IMAGE_FILE_DLL == 0
}
