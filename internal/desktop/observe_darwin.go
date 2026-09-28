//go:build darwin && cgo

package desktop

/*
#include "observe_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type observationContext struct {
	Desktop struct {
		ID       string              `json:"id"`
		Frame    desktopcontrol.Rect `json:"frame"`
		WorkArea desktopcontrol.Rect `json:"workArea"`
	} `json:"desktop"`
	Actor        desktopcontrol.Rect `json:"actor"`
	ActiveWindow struct {
		Application string               `json:"application"`
		ID          *int                 `json:"id"`
		Frame       *desktopcontrol.Rect `json:"frame"`
	} `json:"activeWindow"`
}

func (d *macDriver) observationContext() (observationContext, error) {
	raw := application.InvokeSyncWithResult(func() string {
		p := C.bot_observation_context(d.pointer)
		defer C.free(unsafe.Pointer(p))
		return C.GoString(p)
	})
	var out observationContext
	if json.Unmarshal([]byte(raw), &out) != nil || out.Desktop.ID == "" {
		return out, errors.New("POC observation requires one available display")
	}
	return out, nil
}
func (d *macDriver) observeDesktop(ctx context.Context) (desktopcontrol.Frame, error) {
	if err := ctx.Err(); err != nil {
		return desktopcontrol.Frame{}, err
	}
	before, err := d.observationContext()
	if err != nil {
		return desktopcontrol.Frame{}, err
	}
	id, err := strconv.ParseUint(before.Desktop.ID, 10, 32)
	if err != nil {
		return desktopcontrol.Frame{}, errors.New("display unavailable")
	}
	started := time.Now()
	p := C.bot_observation_image(C.uint32_t(id), C.int(before.Desktop.Frame.Width), C.int(before.Desktop.Frame.Height))
	defer C.free(unsafe.Pointer(p))
	encoded := C.GoString(p)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return desktopcontrol.Frame{}, errors.New("desktop observation: " + encoded)
	}
	if err = ctx.Err(); err != nil {
		return desktopcontrol.Frame{}, err
	}
	after, err := d.observationContext()
	if err != nil {
		return desktopcontrol.Frame{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return desktopcontrol.Frame{}, errors.New("desktop geometry changed during capture; observe again")
	}
	return desktopcontrol.Frame{World: desktopcontrol.World{Display: before.Desktop.ID, Frame: before.Desktop.Frame, WorkArea: before.Desktop.WorkArea, Actor: before.Actor, Window: before.ActiveWindow.Frame, Application: before.ActiveWindow.Application}, Image: data, MIME: "image/jpeg", CapturedAt: started}, nil
}
