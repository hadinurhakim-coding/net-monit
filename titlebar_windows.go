//go:build windows

package main

import (
	"math"
	"syscall"
	"unsafe"
)

var (
	modUser32             = syscall.NewLazyDLL("user32.dll")
	procFindWindowW       = modUser32.NewProc("FindWindowW")
	procGetWindowLongPtrW = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = modUser32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW   = modUser32.NewProc("CallWindowProcW")
	procGetWindowRect     = modUser32.NewProc("GetWindowRect")
	procGetDpiForWindow   = modUser32.NewProc("GetDpiForWindow")

	origWndProc uintptr
	wndProcCb   = syscall.NewCallback(snapWndProc)
)

const (
	wmNchittest = 0x0084
	htMaxButton = 9
	gwlpWndproc = ^uintptr(3)  // GWLP_WNDPROC = -4
	gwlStyle    = ^uintptr(15) // GWL_STYLE = -16
	wsMaximize  = 0x00010000   // WS_MAXIMIZEBOX
	wsThick     = 0x00040000   // WS_THICKFRAME

	// Title bar layout — must match +layout.svelte (Tailwind logical px)
	tbH     = 40.0 // h-10
	btnSize = 32.0 // w-8
	btnGap  = 2.0  // gap-0.5
	padR    = 12.0 // px-3
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func snapWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmNchittest {
		mx := int32(int16(lParam))
		my := int32(int16(lParam >> 16))

		var wr winRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))

		dpi, _, _ := procGetDpiForWindow.Call(hwnd)
		if dpi == 0 {
			dpi = 96
		}
		s := float64(dpi) / 96.0

		// Physical-pixel title bar height
		tbHpx := int32(math.Round(tbH * s))

		// Maximize button screen rect (second from right):
		// [window.Right - padR - closeBtn - gap - maxBtn ... ]
		closeR := wr.Right - int32(math.Round(padR*s))
		maxR := closeR - int32(math.Round(btnSize*s)) - int32(math.Round(btnGap*s))
		maxL := maxR - int32(math.Round(btnSize*s))

		if mx >= maxL && mx <= maxR && my >= wr.Top && my < wr.Top+tbHpx {
			return htMaxButton
		}
	}

	ret, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wParam, lParam)
	return ret
}

func (a *App) initSnapLayout() {
	titlePtr, err := syscall.UTF16PtrFromString("net-monit")
	if err != nil || origWndProc != 0 {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}

	// Ensure WS_MAXIMIZEBOX is set — required for Windows 11 snap layouts.
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlStyle)
	procSetWindowLongPtrW.Call(hwnd, gwlStyle, style|wsMaximize|wsThick)

	// Subclass WNDPROC.
	orig, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlpWndproc)
	if orig == 0 {
		return
	}
	origWndProc = orig
	procSetWindowLongPtrW.Call(hwnd, gwlpWndproc, wndProcCb)
}

// SetMaximizeButtonRect kept for ABI compatibility; geometry is now computed in Go.
func (a *App) SetMaximizeButtonRect(left, top, right, bottom int32) {}
