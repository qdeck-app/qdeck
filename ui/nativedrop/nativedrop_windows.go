//go:build windows

package nativedrop

import (
	"log/slog"
	"math"
	"sync/atomic"
	"syscall"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

// COM result codes.
const (
	sOK          uintptr = 0
	sFalse       uintptr = 1 // OleInitialize returns S_FALSE if already initialized.
	eNoInterface uintptr = 0x80004002
)

// Drag-and-drop constants.
const (
	dropEffectCopy  = 1
	cfHDROP         = 15
	tymedHGlobal    = 1
	dvaspectContent = 1
	queryFileCount  = 0xFFFFFFFF
)

// IDropTarget COM interface identifier Data1 field.
const iidIDropTargetData1 = 0x00000122

//nolint:gochecknoglobals // Windows DLL handles must be package-level.
var (
	ole32   = syscall.NewLazyDLL("ole32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")
	user32  = syscall.NewLazyDLL("user32.dll")

	procOleInitialize    = ole32.NewProc("OleInitialize")
	procOleUninitialize  = ole32.NewProc("OleUninitialize")
	procRegisterDragDrop = ole32.NewProc("RegisterDragDrop")
	procRevokeDragDrop   = ole32.NewProc("RevokeDragDrop")
	procReleaseStgMedium = ole32.NewProc("ReleaseStgMedium")
	procDragQueryFileW   = shell32.NewProc("DragQueryFileW")
	procScreenToClient   = user32.NewProc("ScreenToClient")
)

// guid is a COM globally unique identifier.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

//nolint:gochecknoglobals // COM interface identifiers are package-level constants.
var (
	iidIUnknown    = guid{Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDropTarget = guid{
		Data1: iidIDropTargetData1,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
)

// formatETC matches Windows FORMATETC struct layout.
// Go's natural alignment matches the C layout on both 32-bit and 64-bit.
type formatETC struct {
	CfFormat uint16
	Ptd      uintptr
	Aspect   uint32
	Lindex   int32
	Tymed    uint32
}

// Compile-time assertion: formatETC must match C FORMATETC layout (32 bytes on amd64).
const _formatETCSize = unsafe.Sizeof(formatETC{})

var (
	_ [_formatETCSize - 32]byte //nolint:unused // compile-time size check (fails if too small)
	_ [32 - _formatETCSize]byte //nolint:unused // compile-time size check (fails if too large)
)

// stgMedium matches Windows STGMEDIUM struct layout.
type stgMedium struct {
	Tymed   uint32
	HGlobal uintptr
	PUnk    uintptr
}

// Compile-time assertion: stgMedium must match C STGMEDIUM layout (24 bytes on amd64).
const _stgMediumSize = unsafe.Sizeof(stgMedium{})

var (
	_ [_stgMediumSize - 24]byte //nolint:unused // compile-time size check (fails if too small)
	_ [24 - _stgMediumSize]byte //nolint:unused // compile-time size check (fails if too large)
)

// dropTargetVtbl is the COM vtable for IDropTarget.
type dropTargetVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	DragEnter      uintptr
	DragOver       uintptr
	DragLeave      uintptr
	Drop           uintptr
}

// dropTarget implements the IDropTarget COM interface.
// The first field must be the vtable pointer, per COM convention.
type dropTarget struct {
	vtbl *dropTargetVtbl
	refs uint32
	ndt  *Target
}

//nolint:gochecknoglobals // COM vtable singleton created once at init.
var sharedVtbl = &dropTargetVtbl{
	QueryInterface: syscall.NewCallback(comQueryInterface),
	AddRef:         syscall.NewCallback(comAddRef),
	Release:        syscall.NewCallback(comRelease),
	DragEnter:      syscall.NewCallback(comDragEnter),
	DragOver:       syscall.NewCallback(comDragOver),
	DragLeave:      syscall.NewCallback(comDragLeave),
	Drop:           syscall.NewCallback(comDrop),
}

// IUnknown methods

func comQueryInterface(this, riid, ppvObject uintptr) uintptr {
	id := (*guid)(unsafe.Pointer(riid)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.

	if *id != iidIUnknown && *id != iidIDropTarget {
		*(*uintptr)(unsafe.Pointer(ppvObject)) = 0 //nolint:gosec,govet // COM interface requires unsafe pointer cast.

		return eNoInterface
	}

	*(*uintptr)(unsafe.Pointer(ppvObject)) = this //nolint:gosec,govet // COM interface requires unsafe pointer cast.

	dt := (*dropTarget)(unsafe.Pointer(this)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.
	atomic.AddUint32(&dt.refs, 1)

	return sOK
}

func comAddRef(this uintptr) uintptr {
	dt := (*dropTarget)(unsafe.Pointer(this)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.

	return uintptr(atomic.AddUint32(&dt.refs, 1))
}

func comRelease(this uintptr) uintptr {
	dt := (*dropTarget)(unsafe.Pointer(this)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.
	n := atomic.AddUint32(&dt.refs, ^uint32(0))

	// In normal operation refs never reaches 0 because we hold the initial
	// reference for the lifetime of the drop target. Nil the vtbl pointer
	// as a safety measure to cause an obvious crash if a stale pointer is used.
	if n == 0 {
		dt.vtbl = nil
	}

	return uintptr(n)
}

// IDropTarget methods

func comDragEnter(this, pDataObj, grfKeyState, pt, pdwEffect uintptr) uintptr {
	dt := (*dropTarget)(unsafe.Pointer(this)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.
	dt.ndt.sendDragState(DragEntered)

	*(*uint32)(unsafe.Pointer(pdwEffect)) = dropEffectCopy //nolint:gosec,govet // COM interface requires unsafe pointer cast.

	return sOK
}

func comDragOver(this, grfKeyState, pt, pdwEffect uintptr) uintptr {
	*(*uint32)(unsafe.Pointer(pdwEffect)) = dropEffectCopy //nolint:gosec,govet // COM interface requires unsafe pointer cast.

	return sOK
}

func comDragLeave(this uintptr) uintptr {
	dt := (*dropTarget)(unsafe.Pointer(this)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.
	dt.ndt.sendDragState(DragNone)

	return sOK
}

func comDrop(this, pDataObj, grfKeyState, pt, pdwEffect uintptr) uintptr {
	dt := (*dropTarget)(unsafe.Pointer(this)) //nolint:gosec,govet // COM interface requires unsafe pointer cast.
	dt.ndt.sendDragState(DragNone)

	// POINTL contains screen coordinates. Convert to client-relative via ScreenToClient.
	dropY := screenToClientY(dt.ndt.platform.hwnd, pt)

	paths := extractDropPaths(pDataObj)
	if len(paths) > 0 {
		dt.ndt.sendDrop(paths, dropY)
	}

	*(*uint32)(unsafe.Pointer(pdwEffect)) = dropEffectCopy //nolint:gosec,govet // COM interface requires unsafe pointer cast.

	return sOK
}

// extractDropPaths reads file paths from an IDataObject using CF_HDROP format.
func extractDropPaths(pDataObj uintptr) []string {
	fmtetc := formatETC{
		CfFormat: cfHDROP,
		Aspect:   dvaspectContent,
		Lindex:   -1,
		Tymed:    tymedHGlobal,
	}

	var stgmed stgMedium

	// IDataObject vtable layout: [QueryInterface, AddRef, Release, GetData, ...].
	vtblPtr := *(*uintptr)(unsafe.Pointer(pDataObj))                                //nolint:gosec,govet // COM vtable access.
	getDataFn := *(*uintptr)(unsafe.Pointer(vtblPtr + 3*unsafe.Sizeof(uintptr(0)))) //nolint:gosec,govet,mnd // GetData is at vtable index 3.

	ret, _, _ := syscall.SyscallN(getDataFn,
		pDataObj,
		uintptr(unsafe.Pointer(&fmtetc)), //nolint:gosec // Passing stack struct to COM syscall.
		uintptr(unsafe.Pointer(&stgmed)), //nolint:gosec // Passing stack struct to COM syscall.
	)
	if ret != sOK {
		return nil
	}

	defer releaseStgMedium(&stgmed)

	return queryDropFiles(stgmed.HGlobal)
}

// releaseStgMedium frees a STGMEDIUM structure. ReleaseStgMedium is void in C,
// so the syscall return values carry no meaningful status.
func releaseStgMedium(stgmed *stgMedium) {
	ret, _, _ := procReleaseStgMedium.Call(uintptr(unsafe.Pointer(stgmed))) //nolint:gosec // Passing struct pointer to COM cleanup call.
	_ = ret
}

// queryDropFiles extracts file paths from an HDROP handle using DragQueryFileW.
func queryDropFiles(hDrop uintptr) []string {
	count, _, _ := procDragQueryFileW.Call(hDrop, queryFileCount, 0, 0)
	if count == 0 {
		return nil
	}

	paths := make([]string, 0, count)

	var buf []uint16

	for i := uintptr(0); i < count; i++ {
		// Query required buffer length (excluding null terminator).
		n, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0)
		if n == 0 || n > math.MaxInt32 {
			continue
		}

		needed := int(n) + 1
		if cap(buf) < needed {
			buf = make([]uint16, needed)
		} else {
			buf = buf[:needed]
		}

		ret, _, _ := procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), n+1) //nolint:gosec // Passing buffer to Win32 API.
		_ = ret

		paths = append(paths, syscall.UTF16ToString(buf))
	}

	return paths
}

// screenToClientY converts POINTL screen coordinates to a client-relative Y value.
// POINTL packs x in the lower 32 bits and y in the upper 32 bits.
func screenToClientY(hwnd, pt uintptr) float32 {
	// point matches the Win32 POINT struct: {x int32, y int32}.
	type point struct{ x, y int32 }

	p := point{
		x: int32(pt & 0xFFFFFFFF),         //nolint:gosec,mnd // extract low 32 bits
		y: int32((pt >> 32) & 0xFFFFFFFF), //nolint:gosec,mnd // extract high 32 bits
	}

	procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&p))) //nolint:gosec,errcheck // Win32 BOOL return; point is valid.

	return float32(p.y)
}

// platformState holds Windows-specific drop target state.
type platformState struct {
	dt             *dropTarget
	registered     bool
	oleInitialized bool
	hwnd           uintptr
}

func newPlatformState(t *Target) platformState {
	return platformState{
		dt: &dropTarget{
			vtbl: sharedVtbl,
			refs: 1,
			ndt:  t,
		},
	}
}

// revokeDragDrop unregisters the drop target and logs on failure.
func revokeDragDrop(hwnd uintptr) {
	ret, _, _ := procRevokeDragDrop.Call(hwnd)
	if ret != sOK {
		slog.Warn("RevokeDragDrop failed", "hresult", ret)
	}
}

// ListenEvents captures the Win32ViewEvent and registers the IDropTarget.
//
// OLE initialization and RegisterDragDrop run on the driver thread (via
// window.Run) so the COM STA lives on the thread with the Win32 message
// pump.  Without this, COM dispatches IDropTarget callbacks to the client
// goroutine's thread, which has no message pump, freezing the source
// application during drag-and-drop.
func (t *Target) ListenEvents(evt event.Event) {
	e, ok := evt.(app.Win32ViewEvent)
	if !ok {
		return
	}

	if e.HWND != 0 && !t.platform.registered {
		t.platform.hwnd = e.HWND

		t.window.Run(func() {
			ret, _, _ := procOleInitialize.Call(0)
			if ret != sOK && ret != sFalse {
				slog.Warn("OleInitialize failed, drag-and-drop unavailable", "hresult", ret)

				return
			}

			t.platform.oleInitialized = true

			r2, _, _ := procRegisterDragDrop.Call(e.HWND, uintptr(unsafe.Pointer(t.platform.dt))) //nolint:gosec // COM requires passing struct pointer.
			if r2 != sOK {
				slog.Warn("RegisterDragDrop failed", "hresult", r2)

				return
			}

			t.platform.registered = true
			t.DropSupported = true
		})
	} else if e.HWND == 0 && t.platform.registered {
		t.window.Run(func() {
			revokeDragDrop(t.platform.hwnd)
			t.platform.registered = false
			t.DropSupported = false
		})
	}
}

// Close revokes the COM drag-drop registration. Safe to call if ListenEvents
// already handled HWND=0 (the operation is idempotent).  Runs on the driver
// thread to match the STA that owns the registration.
func (t *Target) Close() {
	t.window.Run(func() {
		if t.platform.registered {
			revokeDragDrop(t.platform.hwnd)
			t.platform.registered = false
		}

		if t.platform.oleInitialized {
			// OleUninitialize is void in C; the syscall return carries no status.
			ret, _, _ := procOleUninitialize.Call()
			_ = ret

			t.platform.oleInitialized = false
		}
	})
}
