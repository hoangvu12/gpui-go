// Oracle: dump markdown-rs 1.0.0 mdast (kind, start, end, detail) for each case,
// using exactly Base's parse_options() (markdown_ext.rs:392-409).
use markdown::{mdast::Node, to_mdast, ParseOptions};
use std::io::Read;

fn opts(kind: &str) -> ParseOptions {
    let mut o = ParseOptions::gfm();
    o.constructs.frontmatter = kind == "fm";
    o.constructs.math_text = kind != "nomath";
    o.constructs.math_flow = kind != "nomath";
    if kind == "mdx" {
        o.constructs.html_flow = false;
        o.constructs.html_text = false;
        o.constructs.mdx_expression_flow = true;
        o.constructs.mdx_expression_text = true;
        o.constructs.mdx_jsx_flow = true;
        o.constructs.mdx_jsx_text = true;
    }
    o
}

fn detail(n: &Node) -> String {
    match n {
        Node::Text(v) => format!("{:?}", v.value),
        Node::InlineCode(v) => format!("{:?}", v.value),
        Node::InlineMath(v) => format!("{:?}", v.value),
        Node::Math(v) => format!("{:?}", v.value),
        Node::Html(v) => format!("{:?}", v.value),
        Node::Yaml(v) => format!("{:?}", v.value),
        Node::Toml(v) => format!("{:?}", v.value),
        Node::Code(v) => format!("lang={:?} {:?}", v.lang, v.value),
        Node::Heading(v) => format!("depth={}", v.depth),
        Node::List(v) => format!("ordered={} start={:?} spread={}", v.ordered, v.start, v.spread),
        Node::ListItem(v) => format!("checked={:?} spread={}", v.checked, v.spread),
        Node::Link(v) => format!("url={:?} title={:?}", v.url, v.title),
        Node::Image(v) => format!("url={:?} alt={:?} title={:?}", v.url, v.alt, v.title),
        Node::FootnoteReference(v) => format!("id={:?}", v.identifier),
        Node::FootnoteDefinition(v) => format!("id={:?}", v.identifier),
        Node::Definition(v) => format!("id={:?} url={:?} title={:?}", v.identifier, v.url, v.title),
        Node::LinkReference(v) => format!("id={:?}", v.identifier),
        Node::ImageReference(v) => format!("id={:?}", v.identifier),
        Node::Table(v) => format!("align={:?}", v.align),
        Node::MdxTextExpression(v) => format!("{:?}", v.value),
        Node::MdxFlowExpression(v) => format!("{:?}", v.value),
        _ => String::new(),
    }
}

fn kind(n: &Node) -> String {
    let s = format!("{:?}", n);
    s.split(|c: char| c == '(' || c == ' ' || c == '{').next().unwrap().to_string()
}

fn walk(n: &Node, depth: usize, out: &mut Vec<String>) {
    let (s, e) = n
        .position()
        .map(|p| (p.start.offset as i64, p.end.offset as i64))
        .unwrap_or((-1, -1));
    out.push(format!("{}{} {}..{} {}", "  ".repeat(depth), kind(n), s, e, detail(n)));
    if let Some(ch) = n.children() {
        for c in ch {
            walk(c, depth + 1, out);
        }
    }
}

fn main() {
    let mut buf = String::new();
    std::io::stdin().read_to_string(&mut buf).unwrap();
    let cases: Vec<serde_json::Value> = serde_json::from_str(&buf).unwrap();
    let mut result = serde_json::Map::new();
    for c in cases {
        let name = c["name"].as_str().unwrap();
        let src = c["src"].as_str().unwrap();
        let o = opts(c["opts"].as_str().unwrap());
        let mut out = Vec::new();
        match to_mdast(src, &o) {
            Ok(root) => walk(&root, 0, &mut out),
            Err(e) => out.push(format!("ERROR {}", e)),
        }
        result.insert(name.to_string(), serde_json::Value::from(out));
    }
    println!("{}", serde_json::to_string_pretty(&result).unwrap());
}
