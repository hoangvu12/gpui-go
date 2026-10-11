import sys

# ============ renderer.go: table mirror + new entries ============
p = 'internal/native/renderer.go'
s = open(p, encoding='utf-8', newline='').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"renderer needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

rep('''type rendererTable struct {
	surfaceCreate      uintptr
	surfaceResize      uintptr
	surfaceInfo        uintptr
	surfacePresent     uintptr
	surfaceDestroy     uintptr
	submissionPoll     uintptr
	submissionRetire   uintptr
	deviceInfo         uintptr
	surfaceDrawScene   uintptr
	surfaceRenderScene uintptr
	surfaceReadPixels  uintptr
	serviceVersion     uint32
	sizeOfTable        uint32
	alignOfTable       uint32
	sizeOfSurfaceInfo  uint32
	sizeOfSubmission   uint32
	sizeOfRetire       uint32
	sizeOfDeviceInfo   uint32
	maxPending         uint32
	maxInstanceBuffer  uint32
	pathMultisample    uint32
}''',
'''type rendererTable struct {
	surfaceCreate          uintptr
	surfaceResize          uintptr
	surfaceInfo            uintptr
	surfacePresent         uintptr
	surfaceDestroy         uintptr
	submissionPoll         uintptr
	submissionRetire       uintptr
	deviceInfo             uintptr
	surfaceDrawScene       uintptr
	surfaceRenderScene     uintptr
	surfaceReadPixels      uintptr
	surfaceRenderToImage   uintptr
	deviceStateCheck       uintptr
	deviceInjectLoss       uintptr
	deviceRebuild          uintptr
	deviceMarkDrawable     uintptr
	serviceVersion         uint32
	sizeOfTable            uint32
	alignOfTable           uint32
	sizeOfSurfaceInfo      uint32
	sizeOfSubmission       uint32
	sizeOfRetire           uint32
	sizeOfDeviceInfo       uint32
	sizeOfDeviceState      uint32
	sizeOfRebuildRecord    uint32
	maxPending             uint32
	maxInstanceBuffer      uint32
	pathMultisample        uint32
}''')

rep('''const rendererServiceVersion uint32 = 2''',
'''const rendererServiceVersion uint32 = 3''')

open(p, 'w', encoding='utf-8', newline='').write(s)
print("renderer.go table updated")

# ============ renderer_windows.go: the FFI for the new entries ============
p = 'internal/native/renderer_windows.go'
s = open(p, encoding='utf-8', newline='').read()
print(len(s), "bytes in renderer_windows.go")
