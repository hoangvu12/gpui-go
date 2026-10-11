import sys
p = 'gpui/desktop_windows_test.go'
s = open(p, encoding='utf-8', newline='').read()

# 1) Replace the entire hover drive block (old comments + NOMOVE re-assert
#    + all diagnostics) with the clean full-placement re-drive.
i = s.find('desktopDriveAndWaitFor(t, "the window to be hovered"')
assert i > 0
# The drive ends at the poll closure's closing "})" — find the poll:
j = s.find('}, func() bool {\n\t\thovered, _ := handle.IsHovered()\n\t\treturn hovered\n\t})', i)
assert j > i
new_drive = '''desktopDriveAndWaitFor(t, "the window to be hovered", func() {
		// Re-assert the FULL topmost placement (geometry included —
		// the filedrop drag test's proven re-drive pattern): the
		// post-fullscreen shell state can leave the desktop layer
		// winning the mouse hit tests over a freshly placed window,
		// which makes TrackMouseEvent deliver its leave immediately
		// (the hover flag flickers true→false on every arming).
		// Re-placing the window re-inserts it at the head of the
		// topmost band; the bounded retry keeps driving until the hit
		// test resolves this window again and the hover state holds.
		procSetWindowPos.Call(handle.Hwnd(), ^uintptr(0),
			uintptr(placeLeft), uintptr(placeTop),
			uintptr(placeWidth), uintptr(placeHeight),
			uintptr(swpNoActivate))
		procSetCursorPos.Call(uintptr(insideX), uintptr(insideY))
		procSendMessageW.Call(handle.Hwnd(), uintptr(wmMouseMove), 0, 0)
	'''
s = s[:i] + new_drive + s[j:]

# 2) Capture the placement geometry in the setup closure.
old = '''		procSetWindowPos.Call(handle.Hwnd(), ^uintptr(0),
			uintptr(left), uintptr(top), uintptr(w), uintptr(hgt),
			uintptr(swpNoActivate))
		var wr rect'''
new = '''		procSetWindowPos.Call(handle.Hwnd(), ^uintptr(0),
			uintptr(left), uintptr(top), uintptr(w), uintptr(hgt),
			uintptr(swpNoActivate))
		placeLeft, placeTop, placeWidth, placeHeight = left, top, w, hgt
		var wr rect'''
assert s.count(old) == 1
s = s.replace(old, new)

# 3) Declare the geometry variables.
old = '''	var insideX, insideY, outsideX, outsideY int32
	placed := h.runForegroundSync(func() {'''
new = '''	var insideX, insideY, outsideX, outsideY int32
	var placeLeft, placeTop, placeWidth, placeHeight int32
	placed := h.runForegroundSync(func() {'''
assert s.count(old) == 1
s = s.replace(old, new)

# 4) Remove the leftover diagnostic declarations.
for junk in [
    '''var hrDiag rect

var (
	procGetWindowRgn   = modUser32.NewProc("GetWindowRgn")
	procGetClassNameW  = modUser32.NewProc("GetClassNameW")
)

func windowsClassName(raw []uint16) string {
	n := 0
	for n < len(raw) && raw[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(raw[:n])
}

''',
    '''var (
	procGetWindow         = modUser32.NewProc("GetWindow")
	procMonitorFromPoint  = modUser32.NewProc("MonitorFromPoint")
	procGetDpiForWindow   = modUser32.NewProc("GetDpiForWindow")

''',
    '''	procGetWindow        = modUser32.NewProc("GetWindow")
''',
]:
    if junk in s:
        s = s.replace(junk, '')
if '\t"syscall"\n' in s and 'syscall.' not in s.replace('"syscall"', ''):
    s = s.replace('\t"syscall"\n', '')

open(p, 'w', encoding='utf-8', newline='').write(s)
print('hover drive rewritten')
