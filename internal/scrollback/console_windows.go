// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

package scrollback

import (
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32                        = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo  = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procReadConsoleOutputCharacterW = kernel32.NewProc("ReadConsoleOutputCharacterW")
)

type coord struct{ x, y int16 }
type smallRect struct{ left, top, right, bottom int16 }
type screenBufferInfo struct {
	size       coord
	cursor     coord
	attributes uint16
	window     smallRect
	maxWindow  coord
}

// packCoord packs a COORD (a by-value arg) into the single uintptr the syscall ABI
// expects: x in the low word, y in the high word.
func packCoord(x, y int16) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

// consoleCapture reads the recent console screen directly from CONOUT$ via the
// Console API: the full buffer under classic conhost, just the viewport under
// Windows Terminal/ConPTY (which keeps its own scrollback the inferior can't see).
// Best-effort -- returns "" when not attached to a console or on any error. The
// text is already resolved cells (no ANSI), so it slots straight into Capture's
// shared post-processing.
func consoleCapture(maxLines int) string {
	name, err := syscall.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return ""
	}
	h, err := syscall.CreateFile(name,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)

	var info screenBufferInfo
	if r, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info))); r == 0 {
		return ""
	}
	cols := int(info.size.x)
	last := int(info.cursor.y) // most recently written row
	if cols <= 0 || last < 0 {
		return ""
	}
	first := 0
	if maxLines > 0 && last-maxLines+1 > first {
		first = last - maxLines + 1
	}

	row := make([]uint16, cols)
	var b strings.Builder
	for y := first; y <= last; y++ {
		var read uint32
		r, _, _ := procReadConsoleOutputCharacterW.Call(
			uintptr(h), uintptr(unsafe.Pointer(&row[0])), uintptr(cols),
			packCoord(0, int16(y)), uintptr(unsafe.Pointer(&read)))
		if r == 0 {
			break
		}
		b.WriteString(strings.TrimRight(string(utf16.Decode(row[:read])), " \x00"))
		b.WriteByte('\n')
	}
	return b.String()
}
