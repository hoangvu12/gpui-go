mod envelope;
mod fixtures;
mod trace;

use anyhow::{Context as _, Result, bail};
use std::path::{Path, PathBuf};

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        bail!(
            "usage: gpui-reference-harness <envelope.json> <trace-out.json>\n\
             Reads a fixture envelope and writes the reference trace for it."
        );
    }
    let envelope_path = PathBuf::from(&args[1]);
    let trace_path = PathBuf::from(&args[2]);

    let envelope_bytes = std::fs::read(&envelope_path)
        .with_context(|| format!("reading envelope {}", envelope_path.display()))?;
    let envelope_hash = sha256_hex(&envelope_bytes);
    let envelope = envelope::Envelope::load(&envelope_path)?;

    let events = match envelope.fixture_kind.as_str() {
        "layout-effects-v1" => fixtures::layout_effects::run(&envelope),
        "layout-metrics-v1" => fixtures::layout_metrics::run(&envelope),
        "scene-painting-v1" => fixtures::scene_painting::run(&envelope),
        "paths-filters-v1" => fixtures::paths_filters::run(&envelope),
        "text-geometry-v1" => fixtures::text_geometry::run(&envelope),
        "glyph-raster-v1" => fixtures::glyph_raster::run(&envelope),
        "authoring-counter-v1" => fixtures::authoring_counter::run(&envelope),
        other => bail!("unknown fixture kind {other:?}"),
    };

    let trace = trace::Trace {
        schema: trace::TRACE_SCHEMA.to_string(),
        fixture_id: envelope.fixture_id.clone(),
        fixture_kind: envelope.fixture_kind.clone(),
        envelope_sha256: envelope_hash,
        harness: trace::HarnessInfo {
            harness_version: env!("CARGO_PKG_VERSION").to_string(),
            harness_crate: env!("CARGO_PKG_NAME").to_string(),
            gpui_crate: trace::GPUI_CE_CRATE.to_string(),
            gpui_commit: trace::GPUI_CE_COMMIT.to_string(),
            profile: trace::HARNESS_PROFILE.to_string(),
            features: vec![
                "default".to_string(),
                "test-support".to_string(),
            ],
        },
        events,
    };

    let json = serde_json::to_string_pretty(&trace)
        .context("serializing trace")?;
    std::fs::write(&trace_path, json)
        .with_context(|| format!("writing trace {}", trace_path.display()))?;
    Ok(())
}

fn sha256_hex(bytes: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    let digest = hasher.finalize();
    let mut out = String::with_capacity(64);
    for byte in digest {
        out.push_str(&format!("{:02x}", byte));
    }
    out
}

// Keep the unused-import lint honest: Path is used in doc examples only.
#[allow(dead_code)]
fn _path_marker(_: &Path) {}
