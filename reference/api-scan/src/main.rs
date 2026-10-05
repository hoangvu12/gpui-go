//! gpui-api-scan: enumerates the public API surface of the pinned GPUI-CE
//! source tree.
//!
//! Output schema: `gpui-go/api-scan@1` (see `scan.rs`).
//!
//! The scanner parses the Rust sources with `syn` and resolves module files
//! starting from each crate root, so module paths and `#[cfg]` conditions are
//! recorded faithfully without evaluating features (the ledger records under
//! which profiles an item exists instead).
//!
//! Additionally it records:
//! - function bodies that are `unimplemented!()`/`todo!()` or empty (no-op),
//!   as unsupported/no-op candidates to trace from call sites;
//! - every `cfg(feature = "...")` site in the workspace for the features of
//!   interest (wgpu/custom-GPU, hot-patching, diagnostics, test/inspector).

mod scan;

use anyhow::Result;
use std::path::PathBuf;

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 3 || args.len() > 4 {
        eprintln!(
            "usage: gpui-api-scan <workspace-source-root> <out.json> [source-commit]"
        );
        std::process::exit(2);
    }
    let source_root = PathBuf::from(&args[1]);
    let out_path = PathBuf::from(&args[2]);
    let source_commit = args.get(3).cloned().unwrap_or_default();

    let output = scan::scan_workspace(&source_root, &source_commit)?;
    let json = serde_json::to_string_pretty(&output)?;
    std::fs::write(&out_path, json)?;
    eprintln!(
        "wrote {} ({} crates, {} items, {} unresolved modules)",
        out_path.display(),
        output.crates.len(),
        output.crates.iter().map(|c| c.items.len()).sum::<usize>(),
        output
            .crates
            .iter()
            .flat_map(|c| c.unresolved_modules.iter())
            .count()
    );
    Ok(())
}
