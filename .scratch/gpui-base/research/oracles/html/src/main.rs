// Dumps html5ever 0.27 + RcDom 0.3 trees in a canonical html5lib-like format.
use html5ever::tendril::TendrilSink;
use html5ever::{namespace_url, ns, parse_document, ParseOpts};
use markup5ever_rcdom::{Handle, NodeData, RcDom};
use std::io::{BufRead, Write};

fn nsname(n: &html5ever::Namespace) -> &'static str {
    if *n == ns!(svg) { "svg " } else if *n == ns!(mathml) { "math " } else { "" }
}

fn walk(h: &Handle, depth: usize, out: &mut String) {
    let pad = "  ".repeat(depth);
    match &h.data {
        NodeData::Document => {}
        NodeData::Doctype { name, public_id, system_id } => {
            out.push_str(&format!("| {pad}<!DOCTYPE {name} \"{public_id}\" \"{system_id}\">\n"));
        }
        NodeData::Text { contents } => {
            out.push_str(&format!("| {pad}\"{}\"\n", contents.borrow()));
        }
        NodeData::Comment { contents } => {
            out.push_str(&format!("| {pad}<!-- {contents} -->\n"));
        }
        NodeData::ProcessingInstruction { .. } => out.push_str(&format!("| {pad}<?pi>\n")),
        NodeData::Element { name, attrs, template_contents, .. } => {
            out.push_str(&format!("| {pad}<{}{}>\n", nsname(&name.ns), name.local));
            let mut a: Vec<String> = attrs
                .borrow()
                .iter()
                .map(|at| {
                    let p = if at.name.ns == ns!(xlink) { "xlink " } else if at.name.ns == ns!(xml) { "xml " } else if at.name.ns == ns!(xmlns) { "xmlns " } else { "" };
                    format!("{p}{}=\"{}\"", at.name.local, at.value)
                })
                .collect();
            a.sort();
            for s in a {
                out.push_str(&format!("| {pad}  {s}\n"));
            }
            if let Some(tc) = template_contents.borrow().as_ref() {
                out.push_str(&format!("| {pad}  content\n"));
                for c in tc.children.borrow().iter() {
                    walk(c, depth + 2, out);
                }
            }
        }
    }
    let d = if matches!(h.data, NodeData::Document) { depth } else { depth + 1 };
    for c in h.children.borrow().iter() {
        walk(c, d, out);
    }
}

fn main() {
    let stdin = std::io::stdin();
    let mut out = std::io::BufWriter::new(std::io::stdout());
    for line in stdin.lock().lines() {
        let s: String = serde_json::from_str(&line.unwrap()).unwrap();
        let dom = parse_document(RcDom::default(), ParseOpts::default())
            .from_utf8()
            .read_from(&mut s.as_bytes())
            .unwrap();
        let mut o = String::new();
        walk(&dom.document, 0, &mut o);
        writeln!(out, "{}", serde_json::to_string(&o).unwrap()).unwrap();
    }
}
