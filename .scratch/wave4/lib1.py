import sys
p = 'reference/native/src/lib.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    assert s.count(old) == count, f"lib needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# 1) module docs + modules
rep('''pub mod atlas;
pub mod glyph;
pub mod image;
pub mod layout;
pub mod renderer;
pub mod scene;
pub mod svg;
pub mod text;''',
'''pub mod a11y;
pub mod atlas;
pub mod capture;
pub mod glyph;
pub mod image;
pub mod layout;
pub mod renderer;
pub mod scene;
pub mod svg;
pub mod text;''')

# 2) revision
rep('''/// change, independently of `abi_version`). Ticket02 shipped revision 1;
/// ticket06 (layout service) bumped it to 2; ticket07 (renderer service)
/// bumped it to 3; ticket08 (scene kernel service) bumped it to 4;
/// ticket09 (text geometry service) bumped it to 5; ticket10 (glyph
/// raster + atlas services, scene sprite primitives) bumped it to 6;
/// ticket16 (path primitives + the pinned PathBuilder tessellation in
/// the scene service, the scene drawing pipeline in the renderer
/// service) bumped it to 7.
pub const GPUI_GO_NATIVE_REVISION: u32 = 9;''',
'''/// Ticket09 (text geometry service) bumped it to 5; ticket10 (glyph
/// raster + atlas services, scene sprite primitives) bumped it to 6;
/// ticket16 (path primitives + the pinned PathBuilder tessellation in
/// the scene service, the scene drawing pipeline in the renderer
/// service) bumped it to 7. The wave-4 rebuild (tickets 22/28/29/32 +
/// the ticket-19 SVG-key follow-up) bumped it to 10: the a11y
/// provider service (slot 9, bit 10), the capture service (slot 10,
/// bit 11), the renderer service v3 (the device-loss entries, the
/// render-to-image entry, the real capture surface import) and the
/// atlas `AtlasKey::Svg` variant.
pub const GPUI_GO_NATIVE_REVISION: u32 = 10;''')

# 3) capability bits
rep('''    pub const SVG_RESGV_0_48: u64 = 1 << 9;
    // Reserved for the planned service tables (bits assigned when those
    // tickets land; do not pre-assign):
    //   bit 10+: accessibility/COM service and later
    //   unassigned bits remain 0
}''',
'''    pub const SVG_RESGV_0_48: u64 = 1 << 9;
    /// Bit 10: accessibility provider service, the pinned
    /// `accesskit` 0.24.1 / `accesskit_windows` 0.33.1 UIA adapter
    /// (adapter lifetime, published tree updates with
    /// activation-before-complete-tree behavior, WM_GETOBJECT
    /// handling with the nested-message lock discipline, the queued
    /// action handoff) (ticket22).
    pub const A11Y_ACCESSKIT: u64 = 1 << 10;
    /// Bit 11: capture service, the Windows Graphics Capture producer
    /// on the renderer service's D3D11 device (monitor source
    /// enumeration, the free-threaded frame pool + session, bounded
    /// frame-arrival notifications, foreground checkout/copy into
    /// immutable destination textures, stop/destroy lifecycle) plus
    /// the renderer's real capture surface import in the scene draw
    /// path (ticket28).
    pub const CAPTURE_WGC: u64 = 1 << 11;
    // Reserved for the planned service tables (bits assigned when those
    // tickets land; do not pre-assign):
    //   bit 12+: later services
    //   unassigned bits remain 0
}''')

# 4) the table's reserved slots (fill 9 and 10)
rep('''        ServiceTablePtr(
            &svg::SVG_TABLE as *const svg::GpuiGoSvgTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
    ],''',
'''        ServiceTablePtr(
            &svg::SVG_TABLE as *const svg::GpuiGoSvgTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &a11y::A11Y_TABLE as *const a11y::GpuiGoA11yTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &capture::CAPTURE_TABLE as *const capture::GpuiGoCaptureTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(core::ptr::null()),
    ],''')

# 5) the slot doc comment
rep('''    /// carries the SVG service table (ticket19); slot 0 and slots 9-15
    /// remain null for future services.''',
'''    /// carries the SVG service table (ticket19); slot 9 carries the
    /// a11y provider table (ticket22); slot 10 carries the capture
    /// table (ticket28); slot 0 and slots 11-15 remain null for
    /// future services.''')

# 6) capabilities mask
rep('''        | capabilities::SCENE_DRAW_PATHS
        | capabilities::SVG_RESGV_0_48,
    size_of_table: size_of::<GpuiGoAbiTable>() as u32,''',
'''        | capabilities::SCENE_DRAW_PATHS
        | capabilities::SVG_RESGV_0_48
        | capabilities::A11Y_ACCESSKIT
        | capabilities::CAPTURE_WGC,
    size_of_table: size_of::<GpuiGoAbiTable>() as u32,''')

# 7) the tests: identity + slot assertions
rep('''        assert!(
            !ABI_TABLE.reserved[8].0.is_null(),
            "slot 8 carries the SVG table (ticket19)"
        );
        // Slots 9-15 stay unassigned for future services (slot 0 stays
        // the bootstrap-only null slot).
        for slot in 9..16 {
            assert!(
                ABI_TABLE.reserved[slot].0.is_null(),
                "slot {slot} must stay unassigned"
            );
        }''',
'''        assert!(
            !ABI_TABLE.reserved[8].0.is_null(),
            "slot 8 carries the SVG table (ticket19)"
        );
        assert!(
            !ABI_TABLE.reserved[9].0.is_null(),
            "slot 9 carries the a11y provider table (ticket22)"
        );
        assert!(
            !ABI_TABLE.reserved[10].0.is_null(),
            "slot 10 carries the capture table (ticket28)"
        );
        // Slots 11-15 stay unassigned for future services (slot 0 stays
        // the bootstrap-only null slot).
        for slot in 11..16 {
            assert!(
                ABI_TABLE.reserved[slot].0.is_null(),
                "slot {slot} must stay unassigned"
            );
        }''')

rep('''        assert_eq!(
            ABI_TABLE.capabilities,
            capabilities::BOOTSTRAP_BUFFER_ROUNDTRIP
                | capabilities::LAYOUT_TAFFY_0_13_0
                | capabilities::RENDERER_D3D11
                | capabilities::SCENE_KERNEL
                | capabilities::TEXT_PARLEY_0_11_1
                | capabilities::GLYPH_RASTER_DIRECTWRITE
                | capabilities::ATLAS_D3D11
                | capabilities::SCENE_DRAW_PATHS
                | capabilities::IMAGE_CODECS_IMAGE_0_25
                | capabilities::SVG_RESGV_0_48
        );''',
'''        assert_eq!(
            ABI_TABLE.capabilities,
            capabilities::BOOTSTRAP_BUFFER_ROUNDTRIP
                | capabilities::LAYOUT_TAFFY_0_13_0
                | capabilities::RENDERER_D3D11
                | capabilities::SCENE_KERNEL
                | capabilities::TEXT_PARLEY_0_11_1
                | capabilities::GLYPH_RASTER_DIRECTWRITE
                | capabilities::ATLAS_D3D11
                | capabilities::SCENE_DRAW_PATHS
                | capabilities::IMAGE_CODECS_IMAGE_0_25
                | capabilities::SVG_RESGV_0_48
                | capabilities::A11Y_ACCESSKIT
                | capabilities::CAPTURE_WGC
        );''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("lib.rs done")
