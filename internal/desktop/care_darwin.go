//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework IOKit
#include "care_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func macCareSampler(log *diagnosticlog.Logger) func() care.Sample {
	decoder := careSampleDecoder{write: log.Write}
	return func() care.Sample {
		p := C.bot_care_sample()
		if p == nil {
			return decoder.decode(nil)
		}
		defer C.free(unsafe.Pointer(p))
		return decoder.decode([]byte(C.GoString(p)))
	}
}
