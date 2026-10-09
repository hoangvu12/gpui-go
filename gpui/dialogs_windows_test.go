//go:build windows

package gpui

// Unit checks for the pure mapping helpers of the Win32 dialog port
// (the behavioral corpus lives in internal/dialogspec).

import (
	"testing"
	"unsafe"
)

// TestOpenDialogOptionsMapping pins the PathPromptOptions ->
// FILEOPENDIALOGOPTIONS mapping of file_open_dialog
// (crates/gpui_windows/src/platform.rs:1304-1311): the base is always
// FOS_FILEMUSTEXIST (options.Files is not consulted on Windows), multiple
// adds FOS_ALLOWMULTISELECT and directories adds FOS_PICKFOLDERS.
func TestOpenDialogOptionsMapping(t *testing.T) {
	cases := []struct {
		name    string
		options PathPromptOptions
		want    uintptr
	}{
		{"base is FILEMUSTEXIST even with no flags", PathPromptOptions{}, uintptr(fosFileMustExist)},
		{"files does not change the Windows flags", PathPromptOptions{Files: true}, uintptr(fosFileMustExist)},
		{"multiple adds ALLOWMULTISELECT", PathPromptOptions{Multiple: true}, uintptr(fosFileMustExist | fosAllowMultiSelect)},
		{"directories adds PICKFOLDERS", PathPromptOptions{Directories: true}, uintptr(fosFileMustExist | fosPickFolders)},
		{"multiple and directories combine", PathPromptOptions{Multiple: true, Directories: true}, uintptr(fosFileMustExist | fosAllowMultiSelect | fosPickFolders)},
	}
	for _, tc := range cases {
		if got := openDialogOptions(tc.options); got != tc.want {
			t.Errorf("%s: openDialogOptions = 0x%x, want 0x%x", tc.name, uint32(got), uint32(tc.want))
		}
	}
}

// TestSimplifyWindowsPath pins the dunce::simplified equivalent used for
// the save dialog's canonicalized directory
// (crates/gpui_windows/src/platform.rs:1348-1350).
func TestSimplifyWindowsPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Users\admin`, `C:\Users\admin`},
		{`\\?\C:\Users\admin`, `C:\Users\admin`},
		{`\\?\UNC\server\share\dir`, `\\server\share\dir`},
		{`\\server\share\dir`, `\\server\share\dir`},
	}
	for _, tc := range cases {
		if got := simplifyWindowsPath(tc.in); got != tc.want {
			t.Errorf("simplifyWindowsPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestComStructLayouts pins the ABI record sizes: the GUID (16) and the
// vtable carrier (one pointer).
func TestComStructLayouts(t *testing.T) {
	if unsafe.Sizeof(guid{}) != 16 {
		t.Fatalf("guid size = %d, want 16", unsafe.Sizeof(guid{}))
	}
	if unsafe.Sizeof(comFilterSpec{}) != 16 {
		t.Fatalf("COMDLG_FILTERSPEC size = %d, want 16 (two pointers on amd64)", unsafe.Sizeof(comFilterSpec{}))
	}
}
