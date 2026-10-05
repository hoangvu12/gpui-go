//go:build !windows

package native

import (
	"errors"
	"runtime"
)

// Non-Windows hosts cannot load the Windows AMD64 artifact. The package still
// compiles everywhere so module tooling works; Load fails with
// ErrUnsupportedTarget at host initialization, per the distribution contract
// ("an unsupported target fails clearly at build or host initialization;
// never a silent fallback").

func hostSupported() error {
	return errors.Join(ErrUnsupportedTarget, errors.New("native: running on "+runtime.GOOS+"/"+runtime.GOARCH+", want windows/amd64"))
}

func loadLibraryExRestricted(path string) (uintptr, error) {
	return 0, &OSLoadError{Path: path, Err: hostSupported()}
}

func getProcAddr(module uintptr, name string) (uintptr, error) {
	return 0, errors.New("native: unsupported host for export " + name)
}

func acquirePublicationLock(path string) (*publicationLock, error) {
	return nil, errors.New("native: unsupported host for publication lock " + path)
}

func (l *publicationLock) release() {}

func openVerifiedFile(path string) (*verifiedFile, error) {
	return nil, errors.New("native: unsupported host for verified artifact handle " + path)
}

func (v *verifiedFile) readAll() ([]byte, error) {
	return nil, errors.New("native: unsupported host for verified artifact read")
}

func (v *verifiedFile) close() {}

func loadTableFromModule(module uintptr, path string) (*abiTable, error) {
	return nil, hostSupported()
}

func callRoundTripRaw(fn uintptr, req *bufferRequest, res *bufferResponse) (int32, error) {
	return 0, hostSupported()
}

func callNoArgExport(addr uintptr) (int32, error) {
	return 0, hostSupported()
}
