import sys
p = 'reference/native/src/scene.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    assert s.count(old) == count, f"needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# validation of the surface source payload at insert
rep('''    fn insert_surface(
        &mut self,
        mut rec: GpuiGoSceneSurfaceRecord,
        opacity: f32,
    ) -> Result<(), i32> {
        self.check_insert_capacity(self.surfaces.len())?;''',
'''    fn insert_surface(
        &mut self,
        mut rec: GpuiGoSceneSurfaceRecord,
        opacity: f32,
    ) -> Result<(), i32> {
        if rec.record_size as usize != core::mem::size_of::<GpuiGoSceneSurfaceRecord>() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        // Ticket28: the source payload — tag 0 = the pinned
        // `SurfaceSource::Unsupported` stand-in (reserved must be
        // zero); tag 1 = a capture frame handle in reserved[0]
        // (the pinned `SurfaceSource::WindowsCapture` import), the
        // remaining reserved words zero.
        match rec.source_tag {
            surface_source_kind::UNSUPPORTED => {
                if rec.source_reserved != [0, 0, 0] {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
            }
            surface_source_kind::CAPTURE_FRAME => {
                if rec.source_reserved[0] == 0
                    || rec.source_reserved[1] != 0
                    || rec.source_reserved[2] != 0
                {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
            }
            _ => return Err(scene_status::ERR_BAD_VALUE),
        }
        self.check_insert_capacity(self.surfaces.len())?;''')

# the kind module + doc on the record
rep('''/// The source is opaque payload (tag 0 = the pinned
/// `SurfaceSource::Unsupported` stand-in used by tests); the paired
/// opacity is carried by `scene_insert_surface`'s `opacity_bits` argument
/// and by the parallel array of the surface dump.''',
'''/// The source is opaque payload (tag 0 = the pinned
/// `SurfaceSource::Unsupported` stand-in used by tests; tag 1 = a
/// capture frame handle in `source_reserved[0]`, the pinned
/// `SurfaceSource::WindowsCapture` import — ticket28); the paired
/// opacity is carried by `scene_insert_surface`'s `opacity_bits` argument
/// and by the parallel array of the surface dump.''')

# the kind constants near the record
rep('''/// Surface record. Layout: `order` @0, `bounds` @4, `content_mask` @20,''',
'''/// Surface source kind tags (the ABI's `SurfaceSource` variants).
pub mod surface_source_kind {
    /// The pinned `SurfaceSource::Unsupported` stand-in (the pinned
    /// renderer's draw path bails on it explicitly).
    pub const UNSUPPORTED: u32 = 0;
    /// A native capture frame handle in `source_reserved[0]` (the
    /// pinned `SurfaceSource::WindowsCapture` import — the renderer
    /// service resolves it through the capture service's frame
    /// registry; ticket28).
    pub const CAPTURE_FRAME: u32 = 1;
}

/// Surface record. Layout: `order` @0, `bounds` @4, `content_mask` @20,''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("scene.rs done")
