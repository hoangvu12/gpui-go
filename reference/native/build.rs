//! Build-time shader pipeline for the vendored, patched shader modules
//! (`shaders/`), adapted from the pinned `gpui_render` build.rs (the
//! ce-source checkout at the `254b5dbd…` pin): WGSL generation through
//! wgsl-rs, validation and HLSL sm_5.0 lowering through Naga 24 ("the
//! floor Naga", the version the pinned crate lowers DX11 from, aliased
//! `naga-old` there), and `D3DCompile` (Fxc) to `vs_5_0`/`ps_5_0` DXBC
//! blobs written into `OUT_DIR` and embedded with `include_bytes!`.
//!
//! This build script exists so the pinned ce-source stays unmodified
//! (the frozen oracle) while the rev-15 divergences recorded in
//! `shaders/mod.rs` can ship: consumer app builds and renderer startup
//! never run a shader compiler — only this maintainer `cargo build` does,
//! exactly like the pinned crate's own build.

#[path = "shaders/mod.rs"]
#[allow(dead_code)]
mod shaders;

use std::{env, fs, path::PathBuf};

#[cfg(windows)]
use std::ffi::CString;

fn main() {
    println!("cargo:rerun-if-changed=shaders");

    let out_dir = PathBuf::from(env::var("OUT_DIR").expect("OUT_DIR must be set"));

    write_vendored_bytecode(&out_dir);
}

/// One vendored pipeline to compile: the interface entry and the WGSL
/// source module it is generated from (the pinned `write_native_shaders`
/// module table, restricted to the patched pipelines).
struct VendoredPipeline {
    pipeline: &'static shaders::interface::Pipeline,
    pipeline_path: &'static str,
    source: &'static wgsl_rs::Source,
}

fn shader_source(name: &str, source: &wgsl_rs::Source) -> String {
    source
        .wgsl_source()
        .unwrap_or_else(|error| panic!("failed to generate {name}: {error}"))
}

fn write_vendored_bytecode(out_dir: &std::path::Path) {
    use shaders::interface::*;

    let modules = [
        VendoredPipeline {
            pipeline: &QUADS,
            pipeline_path: "QUADS",
            source: &shaders::quad::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &SMOOTHED_QUADS,
            pipeline_path: "SMOOTHED_QUADS",
            source: &shaders::quad::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &SHADOWS,
            pipeline_path: "SHADOWS",
            source: &shaders::shadow::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &SMOOTHED_SHADOWS,
            pipeline_path: "SMOOTHED_SHADOWS",
            source: &shaders::shadow::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &BLUR_COMPOSITE,
            pipeline_path: "BLUR_COMPOSITE",
            source: &shaders::blur::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &SMOOTHED_BLUR_COMPOSITE,
            pipeline_path: "SMOOTHED_BLUR_COMPOSITE",
            source: &shaders::blur::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &TRANSFORMED_BLUR_COMPOSITE,
            pipeline_path: "TRANSFORMED_BLUR_COMPOSITE",
            source: &shaders::blur::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &BLEND_COMPOSITE,
            pipeline_path: "BLEND_COMPOSITE",
            source: &shaders::blur::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &DROP_SHADOW_COMPOSITE,
            pipeline_path: "DROP_SHADOW_COMPOSITE",
            source: &shaders::blur::WGSL_SOURCE,
        },
        VendoredPipeline {
            pipeline: &BACKDROP_CROP,
            pipeline_path: "BACKDROP_CROP",
            source: &shaders::blur::WGSL_SOURCE,
        },
    ];

    for module in modules {
        let pipeline = module.pipeline;
        let wgsl = shader_source(pipeline.label, module.source);
        let legacy_module = naga::front::wgsl::parse_str(&wgsl)
            .unwrap_or_else(|error| panic!("failed to parse {}: {error}", pipeline.label));
        let legacy_info = naga::valid::Validator::new(
            naga::valid::ValidationFlags::all(),
            naga::valid::Capabilities::all(),
        )
        .validate(&legacy_module)
        .unwrap_or_else(|error| panic!("failed to validate {}: {error}", pipeline.label));

        let hlsl = write_hlsl(&legacy_module, &legacy_info, &wgsl, pipeline);
        let _ = write_dx11_bytecode(out_dir, pipeline.label, &hlsl, pipeline);
        // The blobs land at OUT_DIR/<label>.vs_5_0.dxbc and
        // <label>.ps_5_0.dxbc; `renderer/draw.rs` embeds them with
        // include_bytes!(concat!(env!("OUT_DIR"), "/<label>…")).
    }
}

/// The draw-constants cbuffer register for instanced pipelines (the
/// pinned `dx11_draw_constants_register`; D3D11 never folds
/// `StartInstanceLocation` into `SV_InstanceID`, so Naga's special
/// constants carry the instance base).
fn dx11_draw_constants_register(pipeline: &shaders::interface::Pipeline) -> Option<u32> {
    use shaders::interface::DataLayout;
    match pipeline.data_layout {
        DataLayout::Instances
        | DataLayout::TexturedInstances
        | DataLayout::MonochromeSprites
        | DataLayout::SubpixelSprites => Some(shaders::interface::DX11_DRAW_CONSTANTS_REGISTER),
        DataLayout::NativeOnly | DataLayout::Surface | DataLayout::Blur => None,
    }
}

/// The pinned `write_hlsl`: Naga 24 HLSL sm_5.0 lowering with the
/// group-based binding map (group 0 at its own register, group 1 offset
/// by 2) and the draw-constants cbuffer, then the sm_5.0 lowering of the
/// Naga special constants.
fn write_hlsl(
    module: &naga::Module,
    info: &naga::valid::ModuleInfo,
    wgsl: &str,
    pipeline: &shaders::interface::Pipeline,
) -> String {
    let label = pipeline.label;
    let mut binding_map = naga::back::hlsl::BindingMap::default();
    for (_, variable) in module.global_variables.iter() {
        if let Some(binding) = &variable.binding {
            binding_map.insert(
                binding.clone(),
                naga::back::hlsl::BindTarget {
                    space: 0,
                    register: binding.binding + if binding.group == 0 { 0 } else { 2 },
                    ..Default::default()
                },
            );
        }
    }
    let draw_constants_register = dx11_draw_constants_register(pipeline);
    let options = naga::back::hlsl::Options {
        shader_model: naga::back::hlsl::ShaderModel::V5_0,
        binding_map,
        fake_missing_bindings: false,
        special_constants_binding: draw_constants_register.map(|register| {
            naga::back::hlsl::BindTarget {
                space: 0,
                register,
                ..Default::default()
            }
        }),
        ..Default::default()
    };
    let mut output = String::new();
    naga::back::hlsl::Writer::new(&mut output, &options)
        .write(module, info, None)
        .unwrap_or_else(|error| panic!("failed to generate HLSL for {label}: {error}"));
    match draw_constants_register {
        Some(register) => lower_draw_constants_to_sm50(output, wgsl, label, register),
        None => {
            assert!(
                !wgsl.contains("instance_index"),
                "{label} reads instance_index but has no DX11 draw constants"
            );
            output
        }
    }
}

/// Naga declares its special constants with `ConstantBuffer<T>`, a
/// shader-model 5.1 form that `vs_5_0` rejects. Rewrite that one
/// declaration into the classic `cbuffer` block (the pinned function).
fn lower_draw_constants_to_sm50(hlsl: String, wgsl: &str, label: &str, register: u32) -> String {
    let declaration =
        format!("ConstantBuffer<NagaConstants> _NagaConstants: register(b{register});");
    assert_eq!(
        hlsl.matches(&declaration).count(),
        1,
        "{label}: expected exactly one Naga special-constants declaration"
    );
    assert_eq!(
        hlsl.matches(&format!("register(b{register})")).count(),
        1,
        "{label}: draw-constants register b{register} collides with another cbuffer"
    );
    let lowered = hlsl.replace(
        &declaration,
        &format!(
            "cbuffer DrawConstants : register(b{register}) {{ NagaConstants _NagaConstants; }}"
        ),
    );
    if wgsl.contains("instance_index") {
        assert!(
            lowered.contains("_NagaConstants.first_instance + "),
            "{label}: instance_index must be offset by the draw-constants base"
        );
    }
    lowered
}

/// Compile D3D11 bytecode for one vendored pipeline (the pinned
/// `write_dx11_bytecode`, minus the non-Windows escape hatch: the native
/// artifact is Windows-only). Returns the base path of the two blobs.
#[cfg(windows)]
fn write_dx11_bytecode(
    out_dir: &std::path::Path,
    label: &str,
    source: &str,
    pipeline: &shaders::interface::Pipeline,
) -> PathBuf {
    use windows::{
        Win32::Graphics::Direct3D::{Fxc::D3DCompile, ID3DInclude},
        core::PCSTR,
    };

    fn compile(source: &str, label: &str, entry: &str, profile: &[u8]) -> Vec<u8> {
        let entry = CString::new(entry).expect("shader entry point contains NUL");
        let mut blob = None;
        let mut errors = None;
        unsafe {
            D3DCompile(
                source.as_ptr().cast(),
                source.len(),
                PCSTR::from_raw(b"gpui_go_vendored_shaders.hlsl\0".as_ptr()),
                None,
                None::<&ID3DInclude>,
                PCSTR::from_raw(entry.as_ptr().cast()),
                PCSTR::from_raw(profile.as_ptr()),
                0,
                0,
                &mut blob,
                Some(&mut errors),
            )
        }
        .unwrap_or_else(|error| {
            let details = errors.as_ref().map(|errors| unsafe {
                std::ffi::CStr::from_ptr(errors.GetBufferPointer().cast())
                    .to_string_lossy()
                    .into_owned()
            });
            panic!(
                "failed to compile generated HLSL for {label} ({entry:?}): {error}\n{}",
                details.unwrap_or_default()
            )
        });
        let blob = blob.expect("D3DCompile returned no bytecode");
        unsafe {
            std::slice::from_raw_parts(blob.GetBufferPointer().cast(), blob.GetBufferSize())
                .to_vec()
        }
    }

    let vertex_name = format!("{label}.vs_5_0.dxbc");
    let fragment_name = format!("{label}.ps_5_0.dxbc");
    fs::write(out_dir.join(&vertex_name), &compile(source, label, pipeline.vertex_entry, b"vs_5_0\0"))
        .unwrap_or_else(|error| panic!("failed to write {vertex_name}: {error}"));
    fs::write(
        out_dir.join(&fragment_name),
        &compile(source, label, pipeline.fragment_entry, b"ps_5_0\0"),
    )
    .unwrap_or_else(|error| panic!("failed to write {fragment_name}: {error}"));
    out_dir.join(label)
}

#[cfg(not(windows))]
fn write_dx11_bytecode(
    _out_dir: &std::path::Path,
    _label: &str,
    _source: &str,
    _pipeline: &shaders::interface::Pipeline,
) -> PathBuf {
    panic!("the vendored shader pipeline only targets Windows (x86_64-pc-windows-msvc)")
}
