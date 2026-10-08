package plugins

import (
	"encoding/binary"
	"hash/fnv"
)

// RuntimeDirectoryGeneration fences an index read across both reconnect and
// native conversation replacement. A new session may need to reconnect its
// selected MCP services even when the host selection did not change.
func RuntimeDirectoryGeneration(transport uint64, session string) uint64 {
	h := fnv.New64a()
	var epoch [8]byte
	binary.LittleEndian.PutUint64(epoch[:], transport)
	_, _ = h.Write(epoch[:])
	_, _ = h.Write([]byte(session))
	return h.Sum64()
}
