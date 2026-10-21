use std::io::{BufRead, Write};
use unicode_segmentation::UnicodeSegmentation;
fn main() {
    let stdin = std::io::stdin();
    let mut out = std::io::BufWriter::new(std::io::stdout());
    for line in stdin.lock().lines() {
        let s: String = serde_json::from_str(&line.unwrap()).unwrap();
        let w: Vec<usize> = s.split_word_bound_indices().map(|(i, p)| i + p.len()).collect();
        let g: Vec<usize> = s.grapheme_indices(true).map(|(i, p)| i + p.len()).collect();
        writeln!(out, "{}", serde_json::json!({"w": w, "g": g})).unwrap();
    }
}
