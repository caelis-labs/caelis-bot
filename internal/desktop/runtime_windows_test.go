package desktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsMissingAssetsFailBeforeProductStartup(t *testing.T) {
	if err := Run(nil); err == nil {
		t.Fatal("missing assets accepted")
	}
}

func TestWindowsPassiveShowDoesNotActivateItsWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	offscreen := int32(-32000)
	hwnd, _, err := winUser.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0x80000000, uintptr(offscreen), uintptr(offscreen), 8, 8, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	defer winUser.NewProc("DestroyWindow").Call(hwnd)
	winUser.NewProc("ShowWindow").Call(hwnd, 0)
	for range 3 {
		before, _, _ := winUser.NewProc("GetForegroundWindow").Call()
		showWindowsWithoutActivation(hwnd, func() { winUser.NewProc("ShowWindow").Call(hwnd, 5) })
		if err := showWindowsSurface(hwnd); err != nil {
			t.Fatal(err)
		}
		after, _, _ := winUser.NewProc("GetForegroundWindow").Call()
		if after == hwnd || before != after {
			t.Fatal("passive refresh changed foreground")
		}
		if visible, _, _ := winUser.NewProc("IsWindowVisible").Call(hwnd); visible == 0 {
			t.Fatal("passive surface still hidden")
		}
	}
}

func TestWindowsHiddenLauncherCannotSuppressVisibleSurface(t *testing.T) {
	const helper = "CAELIS_TEST_HIDDEN_SURFACE"
	if os.Getenv(helper) == "1" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		class, _ := windows.UTF16PtrFromString("STATIC")
		offscreen := int32(-32000)
		// An owned, off-screen window verifies native lifecycle without taking
		// over the test desktop. Visual/product acceptance remains separate.
		hwnd, _, err := winUser.NewProc("CreateWindowExW").Call(0x08000000,
			uintptr(unsafe.Pointer(class)), 0, 0x80000000, uintptr(offscreen), uintptr(offscreen), 8, 8, 0, 0, 0, 0)
		if hwnd == 0 {
			t.Fatalf("create owned native fixture: %v", err)
		}
		defer winUser.NewProc("DestroyWindow").Call(hwnd)
		// The console test runner consumes startup show state differently from
		// the GUI executable. Begin with that executable's observed hidden state;
		// native QA separately exercises its first ShowWindow/STARTUPINFO path.
		winUser.NewProc("ShowWindow").Call(hwnd, 0)
		if visible, _, _ := winUser.NewProc("IsWindowVisible").Call(hwnd); visible != 0 {
			t.Fatal("owned fixture was not hidden")
		}
		if err := showWindowsSurface(hwnd); err != nil {
			t.Fatal(err)
		}
		if visible, _, _ := winUser.NewProc("IsWindowVisible").Call(hwnd); visible == 0 {
			t.Fatal("saved visible surface still suppressed by launcher")
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWindowsHiddenLauncherCannotSuppressVisibleSurface$")
	cmd.Env = append(os.Environ(), helper+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hidden launcher lifecycle: %v\n%s", err, output)
	}
}

func TestWindowsSilhouetteMatchesRenderedPoseAtScaledDPI(t *testing.T) {
	mask := make([]byte, int(BaseWidth*BaseHeight))
	// Distinct top and bottom limb positions catch an upside-down native region.
	mask[(int(BaseHeight)-1)*int(BaseWidth)+12] = 1
	mask[(int(BaseHeight)-1)*int(BaseWidth)+13] = 1
	mask[150] = 1
	got := windowsMaskRects(mask, 1.5, 1.5)
	want := []windowsMaskRect{{18, 0, 21, 2}, {225, 358, 227, 360}}
	if len(got) != len(want) {
		t.Fatalf("silhouette runs: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("native region no longer covers the rendered limb: got %v, want %v", got[i], want[i])
		}
	}
}
func TestWindowsDIBDecoderBoundsAndOrientation(t *testing.T) {
	b := make([]byte, 48)
	binary.LittleEndian.PutUint32(b, 40)
	binary.LittleEndian.PutUint32(b[4:], 1)
	binary.LittleEndian.PutUint32(b[8:], 2)
	binary.LittleEndian.PutUint16(b[12:], 1)
	binary.LittleEndian.PutUint16(b[14:], 24)
	copy(b[40:], []byte{0, 0, 255, 0, 255, 0, 0, 0})
	data, err := dibPNG(b)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, _, blue, _ := im.At(0, 0).RGBA()
	if r != 0 || blue != 65535 {
		t.Fatal("bottom-up DIB orientation lost")
	}
	if _, err = dibPNG(b[:45]); err == nil {
		t.Fatal("truncated pixels accepted")
	}
	binary.LittleEndian.PutUint32(b[4:], 0x7fffffff)
	if _, err = dibPNG(b); err == nil {
		t.Fatal("oversized DIB accepted")
	}
}
