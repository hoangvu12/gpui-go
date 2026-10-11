import sys
p = 'reference/native/tools/measure/main.go'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"measure needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# 1) revision + comment
rep('''	// Ticket19 (the SVG service in reserved slot 8, the pinned
	// resvg/usvg 0.48.1 stack through the public SvgRenderer paths)
	// landed with native revision 9 and capability bit 9, extending the
	// reserved slot array from 8 to 16 entries (an additive ABI record
	// growth, 152 -> 216 bytes, mirrored in lockstep by the Go loader).
	// Keep these in sync with reference/native/src/lib.rs
	// (GPUI_GO_NATIVE_REVISION and the capabilities module).
	nativeRevision = 9''',
'''	// Ticket19 (the SVG service in reserved slot 8, the pinned
	// resvg/usvg 0.48.1 stack through the public SvgRenderer paths)
	// landed with native revision 9 and capability bit 9, extending the
	// reserved slot array from 8 to 16 entries (an additive ABI record
	// growth, 152 -> 216 bytes, mirrored in lockstep by the Go loader).
	// The wave-4 rebuild (tickets 22/28/29/32 + the ticket-19 SVG-key
	// follow-up) landed with native revision 10: the a11y provider
	// service in slot 9 (bit 10) and the capture service in slot 10
	// (bit 11) — both reusing the existing 16-entry reserved array —
	// plus the renderer service v3 and the AtlasKey::Svg variant.
	// Keep these in sync with reference/native/src/lib.rs
	// (GPUI_GO_NATIVE_REVISION and the capabilities module).
	nativeRevision = 10''')

# 2) capabilities mask + comment
rep('''	// bit 0 bootstrap, bit 1 layout-taffy-0-13-0, bit 2 renderer-d3d11,
	// bit 3 scene-kernel-v1, bit 4 text-parley-0-11-1, bit 5
	// glyph-raster-dwrite, bit 6 glyph-atlas-d3d11, bit 7
	// scene-draw-paths (ticket16), bit 8 image-codecs-image-0-25
	// (ticket17), bit 9 svg-resvg-0-48 (ticket19).
	capabilitiesMask = 1023''',
'''	// bit 0 bootstrap, bit 1 layout-taffy-0-13-0, bit 2 renderer-d3d11,
	// bit 3 scene-kernel-v1, bit 4 text-parley-0-11-1, bit 5
	// glyph-raster-dwrite, bit 6 glyph-atlas-d3d11, bit 7
	// scene-draw-paths (ticket16), bit 8 image-codecs-image-0-25
	// (ticket17), bit 9 svg-resvg-0-48 (ticket19), bit 10
	// a11y-accesskit (ticket22), bit 11 capture-wgc (ticket28).
	capabilitiesMask = 4095''')

# 3) windows features
rep('''var windowsFeatures = []string{
	"Win32_Foundation",
	"Win32_Graphics_Direct3D",
	"Win32_Graphics_Direct3D11",
	"Win32_Graphics_DirectComposition",
	"Win32_Graphics_DirectWrite",
	"Win32_Graphics_Dxgi",
	"Win32_Graphics_Dxgi_Common",
	"Win32_System_LibraryLoader",
	"Win32_UI_WindowsAndMessaging",
}''',
'''var windowsFeatures = []string{
	"Win32_Foundation",
	"Win32_Graphics_Direct3D",
	"Win32_Graphics_Direct3D11",
	"Win32_Graphics_DirectComposition",
	"Win32_Graphics_DirectWrite",
	"Win32_Graphics_Dxgi",
	"Win32_Graphics_Dxgi_Common",
	"Win32_Graphics_Gdi",
	"Win32_System_Com",
	"Win32_System_LibraryLoader",
	"Win32_System_WinRT",
	"Win32_System_WinRT_Direct3D11",
	"Win32_System_WinRT_Graphics_Capture",
	"Win32_UI_WindowsAndMessaging",
	"Graphics_Capture",
	"Graphics_DirectX_Direct3D11",
}''')

# 4) capability name lists (both occurrences)
caps_new = '"bootstrap-buffer-roundtrip", "layout-taffy-0-13-0", "renderer-d3d11", "scene-kernel-v1", "text-parley-0-11-1", "glyph-raster-dwrite", "glyph-atlas-d3d11", "scene-draw-paths", "image-codecs-image-0-25", "svg-resvg-0-48", "a11y-accesskit", "capture-wgc"'
caps_old = '"bootstrap-buffer-roundtrip", "layout-taffy-0-13-0", "renderer-d3d11", "scene-kernel-v1", "text-parley-0-11-1", "glyph-raster-dwrite", "glyph-atlas-d3d11", "scene-draw-paths", "image-codecs-image-0-25", "svg-resvg-0-48"'
assert s.count(caps_old) == 2
s = s.replace(caps_old, caps_new)

# 5) the a11y crate measure struct + wiring
rep('''type svgMeasure struct {''',
'''// a11yMeasure records the resolved AccessKit adapter stack of the a11y
// provider service (ticket22): accesskit (the shared instance the
// gpui-ce path dependency already compiles) and accesskit_windows (the
// UIA platform adapter; the one new crate of the rebuild — resolving
// against the SAME windows 0.62.2 family).
type a11yMeasure struct {
	Accesskit         registryMeasure `json:"accesskit"`
	AccesskitWindows  registryMeasure `json:"accesskit_windows"`
}

type svgMeasure struct {''')

rep('''	Svg                 svgMeasure     `json:"svg"`
	CargoLockSHA256     string         `json:"cargo_lock_sha256"`''',
'''	Svg                 svgMeasure     `json:"svg"`
	A11y                a11yMeasure    `json:"a11y"`
	CargoLockSHA256     string         `json:"cargo_lock_sha256"`''')

rep('''	// The SVG service's resolved resvg/usvg stack (ticket19).
	svg := measureSvg(*lockPath)''',
'''	// The SVG service's resolved resvg/usvg stack (ticket19).
	svg := measureSvg(*lockPath)

	// The a11y provider service's resolved AccessKit stack (ticket22).
	a11y := measureA11y(*lockPath)''')

rep('''		Svg:             svg,
		CargoLockSHA256: lockHex,''',
'''		Svg:             svg,
		A11y:            a11y,
		CargoLockSHA256: lockHex,''')

rep('''		Image:              image,
		Target:             targetTriple,''',
'''		Image:              image,
		A11y:               a11y,
		Target:             targetTriple,''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("measure part 1 done")
