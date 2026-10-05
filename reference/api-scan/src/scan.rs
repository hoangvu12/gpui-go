//! The syn-based public API scanner for the pinned GPUI-CE source tree.

use anyhow::{Context as _, Result};
use serde::Serialize;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use syn::spanned::Spanned;
use syn::visit::Visit;
use syn::{Attribute, Item, Visibility};

pub const SCAN_SCHEMA: &str = "gpui-go/api-scan@1";

#[derive(Serialize, Debug, Default)]
pub struct ScanOutput {
    pub schema: String,
    pub source_commit: String,
    pub crates: Vec<CrateScan>,
    /// Feature name -> every cfg site in the workspace mentioning it.
    pub feature_trace: BTreeMap<String, Vec<CfgSite>>,
}

#[derive(Serialize, Debug, Default)]
pub struct CrateScan {
    /// Package name from Cargo.toml (e.g. "gpui-ce").
    pub package: String,
    /// Library target name (e.g. "gpui").
    pub lib_name: String,
    /// Manifest path relative to the workspace root.
    pub manifest: String,
    /// Library root source file relative to the workspace root.
    pub root_file: String,
    pub items: Vec<ItemRecord>,
    pub unresolved_modules: Vec<String>,
}

#[derive(Serialize, Debug, Clone)]
pub struct ItemRecord {
    /// Syntactic path, e.g. `gpui::app::App::update` or `gpui::Platform::open_window`.
    pub path: String,
    /// One of: module, use, fn, method, impl-method, trait, trait-method,
    /// trait-const, trait-type, struct, field, enum, variant, union, const,
    /// static, type, macro.
    pub kind: String,
    /// Source file relative to the workspace root.
    pub file: String,
    pub line: usize,
    /// Effective cfg conditions: the item's own `#[cfg]`s plus those of all
    /// enclosing modules.
    pub cfg: Vec<String>,
    /// First line of the doc comment, if any.
    pub doc: Option<String>,
    /// For fns and methods with bodies: `unimplemented`, `todo` or `noop`
    /// when the body is a marker macro or empty.
    pub body_marker: Option<String>,
    /// Receiver string for methods (`self`, `&self`, `&mut self`).
    pub receiver: Option<String>,
}

#[derive(Serialize, Debug, Clone, Ord, PartialOrd, PartialEq, Eq)]
pub struct CfgSite {
    pub file: String,
    pub line: usize,
    /// Syntactic path of the nearest enclosing item, or the file stem.
    pub context: String,
    /// The full cfg attribute tokens.
    pub cfg: String,
}

/// Per-module walking context. Cloned for nested modules so each subtree has
/// its own file, path stack and cfg stack.
#[derive(Clone)]
struct ModCtx {
    file_rel: String,
    module_path: Vec<String>,
    cfg_stack: Vec<String>,
}

struct ScanCtx<'a> {
    workspace_root: &'a Path,
    items: Vec<ItemRecord>,
    unresolved: Vec<String>,
}

pub fn scan_workspace(source_root: &Path, source_commit: &str) -> Result<ScanOutput> {
    let workspace_manifest = source_root.join("Cargo.toml");
    let manifest_text = std::fs::read_to_string(&workspace_manifest)
        .with_context(|| format!("reading {}", workspace_manifest.display()))?;
    let workspace: toml::Value = manifest_text.parse()?;
    let members: Vec<String> = workspace
        .get("workspace")
        .and_then(|w| w.get("members"))
        .and_then(|m| m.as_array())
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_str().map(str::to_string))
                .collect()
        })
        .unwrap_or_default();

    let mut output = ScanOutput {
        schema: SCAN_SCHEMA.to_string(),
        source_commit: source_commit.to_string(),
        crates: Vec::new(),
        feature_trace: BTreeMap::new(),
    };

    for member in &members {
        let Some(manifest_path) = normalize_member(source_root, member) else {
            continue;
        };
        if !manifest_path.exists() {
            continue;
        }
        if let Some(mut crate_scan) = scan_crate(source_root, &manifest_path)? {
            output.crates.push(crate_scan);
        }
    }

    output.feature_trace = scan_feature_sites(source_root)?;
    Ok(output)
}

/// Resolve a workspace member string like `./crates/gpui/` into a manifest path.
fn normalize_member(root: &Path, member: &str) -> Option<PathBuf> {
    let trimmed = member.trim_start_matches("./").trim_end_matches('/');
    if trimmed.ends_with(".toml") {
        return Some(root.join(trimmed));
    }
    Some(root.join(trimmed).join("Cargo.toml"))
}

fn scan_crate(workspace_root: &Path, manifest_path: &Path) -> Result<Option<CrateScan>> {
    let text = std::fs::read_to_string(manifest_path)
        .with_context(|| format!("reading {}", manifest_path.display()))?;
    let manifest: toml::Value = text.parse()?;
    let package = manifest
        .get("package")
        .and_then(|p| p.get("name"))
        .and_then(|n| n.as_str())
        .unwrap_or_default()
        .to_string();
    let lib_section = manifest.get("lib");
    let lib_path = lib_section
        .and_then(|l| l.get("path"))
        .and_then(|p| p.as_str())
        .map(str::to_string);
    let lib_name = lib_section
        .and_then(|l| l.get("name"))
        .and_then(|n| n.as_str())
        .map(str::to_string)
        .unwrap_or_else(|| package.replace('-', "_"));

    let crate_root = manifest_path.parent().unwrap().to_path_buf();
    let root_rel = match lib_path {
        Some(p) => p,
        None => {
            let default = crate_root.join("src/lib.rs");
            if default.exists() {
                "src/lib.rs".to_string()
            } else {
                // No library target (e.g. binary-only or tooling crates).
                return Ok(None);
            }
        }
    };
    let root_file = crate_root.join(&root_rel);
    if !root_file.exists() {
        return Ok(None);
    }

    let manifest_rel = relative(workspace_root, manifest_path);
    let root_rel_path = relative(workspace_root, &root_file);

    let mut scan = ScanCtx {
        workspace_root,
        items: Vec::new(),
        unresolved: Vec::new(),
    };
    let mctx = ModCtx {
        file_rel: root_rel_path.clone(),
        module_path: vec![lib_name.clone()],
        cfg_stack: Vec::new(),
    };
    let mut files_parsed: Vec<PathBuf> = Vec::new();
    parse_and_walk(&root_file, &mctx, &mut scan, &mut files_parsed)?;

    Ok(Some(CrateScan {
        package,
        lib_name,
        manifest: manifest_rel,
        root_file: root_rel_path,
        items: scan.items,
        unresolved_modules: scan.unresolved,
    }))
}

fn relative(root: &Path, path: &Path) -> String {
    path.strip_prefix(root)
        .unwrap_or(path)
        .to_string_lossy()
        .replace('\\', "/")
}

fn parse_and_walk(
    file: &Path,
    mctx: &ModCtx,
    scan: &mut ScanCtx,
    files: &mut Vec<PathBuf>,
) -> Result<()> {
    if files.contains(&file.to_path_buf()) {
        return Ok(());
    }
    files.push(file.to_path_buf());
    let text =
        std::fs::read_to_string(file).with_context(|| format!("reading {}", file.display()))?;
    let ast = syn::parse_file(&text)
        .with_context(|| format!("parsing {}", file.display()))?;
    walk_items(&ast.items, mctx, scan, files)
}

fn cfg_strings(attrs: &[Attribute]) -> Vec<String> {
    attrs
        .iter()
        .filter(|attr| attr.path().is_ident("cfg"))
        .map(cfg_text)
        .collect()
}

/// The token text of a cfg attribute (`feature = "x"`, `any(...)` etc.).
fn cfg_text(attr: &Attribute) -> String {
    match &attr.meta {
        syn::Meta::List(list) => list.tokens.to_string(),
        _ => String::new(),
    }
}

fn first_doc(attrs: &[Attribute]) -> Option<String> {
    for attr in attrs {
        if attr.path().is_ident("doc") {
            if let syn::Meta::NameValue(nv) = &attr.meta {
                if let syn::Expr::Lit(syn::ExprLit {
                    lit: syn::Lit::Str(s),
                    ..
                }) = &nv.value
                {
                    let line = s.value().trim().lines().next().unwrap_or("").to_string();
                    if !line.is_empty() {
                        return Some(line);
                    }
                }
            }
        }
    }
    None
}

fn is_public(vis: &Visibility) -> bool {
    matches!(vis, Visibility::Public(_))
}

fn effective_cfg(mctx: &ModCtx, own: &[String]) -> Vec<String> {
    let mut cfg = mctx.cfg_stack.clone();
    cfg.extend(own.iter().cloned());
    cfg
}

fn record(
    scan: &mut ScanCtx,
    mctx: &ModCtx,
    item_path: String,
    kind: &str,
    line: usize,
    cfg: Vec<String>,
    doc: Option<String>,
    body_marker: Option<String>,
    receiver: Option<String>,
) {
    scan.items.push(ItemRecord {
        path: item_path,
        kind: kind.to_string(),
        file: mctx.file_rel.clone(),
        line,
        cfg,
        doc,
        body_marker,
        receiver,
    });
}

fn join_path(mctx: &ModCtx, segments: &[String]) -> String {
    let mut parts = mctx.module_path.clone();
    parts.extend(segments.iter().cloned());
    parts.join("::")
}

fn body_marker(block: &syn::Block) -> Option<String> {
    if block.stmts.is_empty() {
        return Some("noop".to_string());
    }
    for stmt in &block.stmts {
        let mac = match stmt {
            syn::Stmt::Expr(syn::Expr::Macro(mac), _) => Some(&mac.mac),
            syn::Stmt::Macro(sm) => Some(&sm.mac),
            _ => None,
        };
        if let Some(mac) = mac {
            if mac.path.is_ident("unimplemented") {
                return Some("unimplemented".to_string());
            }
            if mac.path.is_ident("todo") {
                return Some("todo".to_string());
            }
        }
    }
    None
}

fn receiver_string(sig: &syn::Signature) -> Option<String> {
    match sig.receiver() {
        Some(syn::Receiver {
            reference: Some(_),
            mutability: Some(_),
            ..
        }) => Some("&mut self".to_string()),
        Some(syn::Receiver {
            reference: Some(_), ..
        }) => Some("&self".to_string()),
        Some(_) => Some("self".to_string()),
        None => None,
    }
}

fn walk_items(
    items: &[Item],
    mctx: &ModCtx,
    scan: &mut ScanCtx,
    files: &mut Vec<PathBuf>,
) -> Result<()> {
    for item in items {
        match item {
            Item::Mod(m) => {
                let own_cfg = cfg_strings(&m.attrs);
                let cfg = effective_cfg(mctx, &own_cfg);
                let line = m.span().start().line;
                record(
                    scan,
                    mctx,
                    join_path(mctx, &[m.ident.to_string()]),
                    "module",
                    line,
                    cfg,
                    first_doc(&m.attrs),
                    None,
                    None,
                );
                match &m.content {
                    Some((_, inner)) => {
                        let mut inner_mctx = mctx.clone();
                        inner_mctx.module_path.push(m.ident.to_string());
                        inner_mctx.cfg_stack.extend(own_cfg);
                        walk_items(inner, &inner_mctx, scan, files)?;
                    }
                    None => {
                        let declaring_file = scan.workspace_root.join(&mctx.file_rel);
                        if let Some(path) =
                            resolve_mod_file(&declaring_file, &m.ident.to_string())
                        {
                            let mut inner_mctx = mctx.clone();
                            inner_mctx.module_path.push(m.ident.to_string());
                            inner_mctx.cfg_stack.extend(own_cfg);
                            inner_mctx.file_rel = relative(scan.workspace_root, &path);
                            parse_and_walk(&path, &inner_mctx, scan, files)?;
                        } else {
                            scan.unresolved.push(format!(
                                "{} (declared in {})",
                                join_path(mctx, &[m.ident.to_string()]),
                                mctx.file_rel
                            ));
                        }
                    }
                }
            }
            Item::Fn(f) => {
                if is_public(&f.vis) {
                    let own_cfg = cfg_strings(&f.attrs);
                    let marker = body_marker(&f.block);
                    record(
                        scan,
                        mctx,
                        join_path(mctx, &[f.sig.ident.to_string()]),
                        "fn",
                        f.span().start().line,
                        effective_cfg(mctx, &own_cfg),
                        first_doc(&f.attrs),
                        marker,
                        receiver_string(&f.sig),
                    );
                }
            }
            Item::Struct(s) => {
                if is_public(&s.vis) {
                    let own_cfg = cfg_strings(&s.attrs);
                    let cfg = effective_cfg(mctx, &own_cfg);
                    let path = join_path(mctx, &[s.ident.to_string()]);
                    record(
                        scan,
                        mctx,
                        path.clone(),
                        "struct",
                        s.span().start().line,
                        cfg.clone(),
                        first_doc(&s.attrs),
                        None,
                        None,
                    );
                    record_fields(scan, mctx, &path, &s.fields, &cfg);
                }
            }
            Item::Enum(e) => {
                if is_public(&e.vis) {
                    let own_cfg = cfg_strings(&e.attrs);
                    let cfg = effective_cfg(mctx, &own_cfg);
                    let path = join_path(mctx, &[e.ident.to_string()]);
                    record(
                        scan,
                        mctx,
                        path.clone(),
                        "enum",
                        e.span().start().line,
                        cfg.clone(),
                        first_doc(&e.attrs),
                        None,
                        None,
                    );
                    for variant in &e.variants {
                        let variant_cfg: Vec<String> = cfg_strings(&variant.attrs)
                            .into_iter()
                            .chain(cfg.iter().cloned())
                            .collect();
                        record(
                            scan,
                            mctx,
                            format!("{}::{}", path, variant.ident),
                            "variant",
                            variant.span().start().line,
                            variant_cfg,
                            first_doc(&variant.attrs),
                            None,
                            None,
                        );
                    }
                }
            }
            Item::Union(u) => {
                if is_public(&u.vis) {
                    let own_cfg = cfg_strings(&u.attrs);
                    let cfg = effective_cfg(mctx, &own_cfg);
                    let path = join_path(mctx, &[u.ident.to_string()]);
                    record(
                        scan,
                        mctx,
                        path.clone(),
                        "union",
                        u.span().start().line,
                        cfg.clone(),
                        first_doc(&u.attrs),
                        None,
                        None,
                    );
                }
            }
            Item::Trait(t) => {
                if is_public(&t.vis) {
                    let own_cfg = cfg_strings(&t.attrs);
                    let cfg = effective_cfg(mctx, &own_cfg);
                    let path = join_path(mctx, &[t.ident.to_string()]);
                    record(
                        scan,
                        mctx,
                        path.clone(),
                        "trait",
                        t.span().start().line,
                        cfg.clone(),
                        first_doc(&t.attrs),
                        None,
                        None,
                    );
                    for trait_item in &t.items {
                        match trait_item {
                            syn::TraitItem::Fn(f) => {
                                let item_cfg: Vec<String> = cfg_strings(&f.attrs)
                                    .into_iter()
                                    .chain(cfg.iter().cloned())
                                    .collect();
                                record(
                                    scan,
                                    mctx,
                                    format!("{}::{}", path, f.sig.ident),
                                    "trait-method",
                                    f.span().start().line,
                                    item_cfg,
                                    first_doc(&f.attrs),
                                    f.default.as_ref().and_then(|b| body_marker(b)),
                                    receiver_string(&f.sig),
                                );
                            }
                            syn::TraitItem::Const(c) => {
                                let item_cfg: Vec<String> = cfg_strings(&c.attrs)
                                    .into_iter()
                                    .chain(cfg.iter().cloned())
                                    .collect();
                                record(
                                    scan,
                                    mctx,
                                    format!("{}::{}", path, c.ident),
                                    "trait-const",
                                    c.span().start().line,
                                    item_cfg,
                                    first_doc(&c.attrs),
                                    None,
                                    None,
                                );
                            }
                            syn::TraitItem::Type(ty) => {
                                let item_cfg: Vec<String> = cfg_strings(&ty.attrs)
                                    .into_iter()
                                    .chain(cfg.iter().cloned())
                                    .collect();
                                record(
                                    scan,
                                    mctx,
                                    format!("{}::{}", path, ty.ident),
                                    "trait-type",
                                    ty.span().start().line,
                                    item_cfg,
                                    first_doc(&ty.attrs),
                                    None,
                                    None,
                                );
                            }
                            _ => {}
                        }
                    }
                }
            }
            Item::Impl(imp) => {
                let self_name = type_path_name(&imp.self_ty);
                let trait_name = imp.trait_.as_ref().map(|(_, path, _)| path_name(path));
                let (owner, kind) = match (&trait_name, &self_name) {
                    (Some(tr), Some(self_ty)) => (format!("{} for {}", tr, self_ty), "impl-method"),
                    (Some(tr), None) => (tr.clone(), "impl-method"),
                    (None, Some(self_ty)) => (self_ty.clone(), "method"),
                    (None, None) => (String::new(), "method"),
                };
                for inner in &imp.items {
                    if let syn::ImplItem::Fn(f) = inner {
                        if !is_public(&f.vis) && trait_name.is_none() {
                            continue;
                        }
                        let mut own_cfg = cfg_strings(&f.attrs);
                        own_cfg.extend(cfg_strings(&imp.attrs));
                        own_cfg.extend(mctx.cfg_stack.iter().cloned());
                        let path = if owner.is_empty() {
                            join_path(mctx, &[f.sig.ident.to_string()])
                        } else {
                            format!("{}::{}::{}", mctx.module_path.join("::"), owner, f.sig.ident)
                        };
                        scan.items.push(ItemRecord {
                            path,
                            kind: kind.to_string(),
                            file: mctx.file_rel.clone(),
                            line: f.span().start().line,
                            cfg: own_cfg,
                            doc: first_doc(&f.attrs),
                            body_marker: body_marker(&f.block),
                            receiver: receiver_string(&f.sig),
                        });
                    }
                }
            }
            Item::Const(c) => {
                if is_public(&c.vis) {
                    let own_cfg = cfg_strings(&c.attrs);
                    record(
                        scan,
                        mctx,
                        join_path(mctx, &[c.ident.to_string()]),
                        "const",
                        c.span().start().line,
                        effective_cfg(mctx, &own_cfg),
                        first_doc(&c.attrs),
                        None,
                        None,
                    );
                }
            }
            Item::Static(s) => {
                if is_public(&s.vis) {
                    let own_cfg = cfg_strings(&s.attrs);
                    record(
                        scan,
                        mctx,
                        join_path(mctx, &[s.ident.to_string()]),
                        "static",
                        s.span().start().line,
                        effective_cfg(mctx, &own_cfg),
                        first_doc(&s.attrs),
                        None,
                        None,
                    );
                }
            }
            Item::Type(t) => {
                if is_public(&t.vis) {
                    let own_cfg = cfg_strings(&t.attrs);
                    record(
                        scan,
                        mctx,
                        join_path(mctx, &[t.ident.to_string()]),
                        "type",
                        t.span().start().line,
                        effective_cfg(mctx, &own_cfg),
                        first_doc(&t.attrs),
                        None,
                        None,
                    );
                }
            }
            Item::Macro(mac) => {
                let is_export = mac.attrs.iter().any(|a| a.path().is_ident("macro_export"));
                if is_export {
                    let ident = mac.ident.as_ref().map(|i| i.to_string()).unwrap_or_default();
                    record(
                        scan,
                        mctx,
                        join_path(mctx, &[ident]),
                        "macro",
                        mac.span().start().line,
                        effective_cfg(mctx, &cfg_strings(&mac.attrs)),
                        first_doc(&mac.attrs),
                        None,
                        None,
                    );
                }
            }
            Item::Use(u) => {
                if is_public(&u.vis) {
                    let mut leaves = Vec::new();
                    collect_use_leaves(&u.tree, &mut leaves);
                    for leaf in leaves {
                        record(
                            scan,
                            mctx,
                            join_path(mctx, &[leaf]),
                            "use",
                            u.span().start().line,
                            effective_cfg(mctx, &cfg_strings(&u.attrs)),
                            None,
                            None,
                            None,
                        );
                    }
                }
            }
            _ => {}
        }
    }
    Ok(())
}

fn record_fields(scan: &mut ScanCtx, mctx: &ModCtx, parent_path: &str, fields: &syn::Fields, cfg: &[String]) {
    match fields {
        syn::Fields::Named(named) => {
            for field in &named.named {
                if is_public(&field.vis) {
                    let name = field
                        .ident
                        .as_ref()
                        .map(|i| i.to_string())
                        .unwrap_or_default();
                    let field_cfg: Vec<String> = cfg_strings(&field.attrs)
                        .into_iter()
                        .chain(cfg.iter().cloned())
                        .collect();
                    record(
                        scan,
                        mctx,
                        format!("{}::{}", parent_path, name),
                        "field",
                        field.span().start().line,
                        field_cfg,
                        first_doc(&field.attrs),
                        None,
                        None,
                    );
                }
            }
        }
        syn::Fields::Unnamed(unnamed) => {
            for (index, field) in unnamed.unnamed.iter().enumerate() {
                if is_public(&field.vis) {
                    let field_cfg: Vec<String> = cfg_strings(&field.attrs)
                        .into_iter()
                        .chain(cfg.iter().cloned())
                        .collect();
                    record(
                        scan,
                        mctx,
                        format!("{}::{}", parent_path, index),
                        "field",
                        field.span().start().line,
                        field_cfg,
                        first_doc(&field.attrs),
                        None,
                        None,
                    );
                }
            }
        }
        syn::Fields::Unit => {}
    }
}

fn collect_use_leaves(tree: &syn::UseTree, out: &mut Vec<String>) {
    match tree {
        syn::UseTree::Path(p) => collect_use_leaves(&p.tree, out),
        syn::UseTree::Name(n) => out.push(n.ident.to_string()),
        syn::UseTree::Rename(r) => out.push(r.rename.to_string()),
        syn::UseTree::Glob(_) => out.push("*".to_string()),
        syn::UseTree::Group(g) => {
            for item in &g.items {
                collect_use_leaves(item, out);
            }
        }
    }
}

fn type_path_name(ty: &syn::Type) -> Option<String> {
    if let syn::Type::Path(p) = ty {
        return Some(path_name(&p.path));
    }
    None
}

fn path_name(path: &syn::Path) -> String {
    path.segments
        .iter()
        .map(|seg| seg.ident.to_string())
        .collect::<Vec<_>>()
        .join("::")
}

/// Resolve `mod foo;` declared in `declaring_file` to its source file.
fn resolve_mod_file(declaring_file: &Path, name: &str) -> Option<PathBuf> {
    let parent = declaring_file.parent()?;
    let file_name = declaring_file.file_name()?.to_str()?;
    let is_root =
        matches!(file_name, "lib.rs" | "main.rs" | "gpui.rs") || file_name == "mod.rs";
    let base = if is_root {
        parent.to_path_buf()
    } else {
        let stem = file_name.trim_end_matches(".rs");
        parent.join(stem)
    };
    let candidates = [
        base.join(format!("{name}.rs")),
        base.join(name).join("mod.rs"),
    ];
    candidates.into_iter().find(|c| c.exists())
}

/// Visitor recording every `cfg` attribute that mentions a feature anywhere
/// in a file's AST, with the nearest enclosing item path as context.
pub struct CfgSiteVisitor<'a> {
    file: &'a str,
    trace: &'a mut BTreeMap<String, Vec<CfgSite>>,
    path_stack: Vec<String>,
}

impl<'a> CfgSiteVisitor<'a> {
    pub fn new(file: &'a str, trace: &'a mut BTreeMap<String, Vec<CfgSite>>) -> Self {
        Self {
            file,
            trace,
            path_stack: Vec::new(),
        }
    }

    fn context(&self) -> String {
        if self.path_stack.is_empty() {
            self.file.to_string()
        } else {
            self.path_stack.join("::")
        }
    }
}

impl<'ast> Visit<'ast> for CfgSiteVisitor<'_> {
    fn visit_item(&mut self, i: &'ast Item) {
        let segment = match i {
            Item::Fn(f) => f.sig.ident.to_string(),
            Item::Struct(s) => s.ident.to_string(),
            Item::Enum(e) => e.ident.to_string(),
            Item::Union(u) => u.ident.to_string(),
            Item::Trait(t) => t.ident.to_string(),
            Item::Mod(m) => m.ident.to_string(),
            Item::Const(c) => c.ident.to_string(),
            Item::Static(s) => s.ident.to_string(),
            Item::Type(t) => t.ident.to_string(),
            Item::Impl(imp) => {
                let self_name = type_path_name(&imp.self_ty).unwrap_or_default();
                match &imp.trait_ {
                    Some((_, path, _)) => {
                        format!("impl {} for {}", path_name(path), self_name)
                    }
                    None => format!("impl {}", self_name),
                }
            }
            Item::Use(u) => {
                let mut leaves = Vec::new();
                collect_use_leaves(&u.tree, &mut leaves);
                leaves.join(",")
            }
            Item::Macro(mac) => mac
                .ident
                .as_ref()
                .map(|i| i.to_string())
                .unwrap_or_default(),
            _ => String::new(),
        };
        let pushed = !segment.is_empty();
        if pushed {
            self.path_stack.push(segment);
        }
        syn::visit::visit_item(self, i);
        if pushed {
            self.path_stack.pop();
        }
    }

    fn visit_attribute(&mut self, attr: &'ast Attribute) {
        if attr.path().is_ident("cfg") {
            let tokens = cfg_text(attr);
            for feature in extract_features(&tokens) {
                let site = CfgSite {
                    file: self.file.to_string(),
                    line: attr.span().start().line,
                    context: self.context(),
                    cfg: tokens.clone(),
                };
                self.trace.entry(feature).or_default().push(site);
            }
        }
        syn::visit::visit_attribute(self, attr);
    }
}

/// Extract every `feature = "name"` occurrence from cfg token text.
pub fn extract_features(cfg_text: &str) -> Vec<String> {
    let mut features = Vec::new();
    let marker = "feature = \"";
    let mut i = 0;
    while let Some(rel) = cfg_text[i..].find(marker) {
        let start = i + rel + marker.len();
        match cfg_text[start..].find('"') {
            Some(len) => {
                features.push(cfg_text[start..start + len].to_string());
                i = start + len + 1;
            }
            None => break,
        }
    }
    features
}

/// Scan every Rust file under the workspace for feature cfg sites.
pub fn scan_feature_sites(source_root: &Path) -> Result<BTreeMap<String, Vec<CfgSite>>> {
    let mut trace: BTreeMap<String, Vec<CfgSite>> = BTreeMap::new();
    for entry in walkdir::WalkDir::new(source_root)
        .into_iter()
        .filter_map(|e| e.ok())
    {
        let path = entry.path();
        if path.extension().and_then(|e| e.to_str()) != Some("rs") {
            continue;
        }
        // Skip generated/build output if any appears under the source root.
        let rel = relative(source_root, path);
        let text = match std::fs::read_to_string(path) {
            Ok(t) => t,
            Err(_) => continue,
        };
        let ast = match syn::parse_file(&text) {
            Ok(ast) => ast,
            Err(_) => continue,
        };
        let mut visitor = CfgSiteVisitor::new(&rel, &mut trace);
        visitor.visit_file(&ast);
    }
    for sites in trace.values_mut() {
        sites.sort();
        sites.dedup();
    }
    Ok(trace)
}
