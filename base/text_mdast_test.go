package base

import (
	"strings"
	"testing"
)

// mdastOracleCases are markdown-rs 1.0.0 `to_mdast` dumps recorded with
// Base's exact parse_options() (U:text/markdown_ext.rs:392-409) by
// .scratch/gpui-base/research/oracles/markdown (src/main.rs, oracle.json).
// The inputs are the markdown.rs test sources (t01-t57, U:text/format/
// markdown.rs:1185-2283) plus GFM coverage inputs (x_*). t08 (8 KiB of
// code) is covered by the ported unit test instead; t14/t15 are MDX,
// which is deferred (they must fail with errMdastMDXUnsupported).
var mdastOracleCases = []struct {
	name, opts, src string
	want            []string
}{
	{name: "t01_same", opts: "default", src: "**same** then **same**", want: []string{
		"Root 0..22 ",
		"  Paragraph 0..22 ",
		"    Strong 0..8 ",
		"      Text 2..6 \"same\"",
		"    Text 8..14 \" then \"",
		"    Strong 14..22 ",
		"      Text 16..20 \"same\"",
	}},
	{name: "t02_same", opts: "default", src: "**same** then **same**", want: []string{
		"Root 0..22 ",
		"  Paragraph 0..22 ",
		"    Strong 0..8 ",
		"      Text 2..6 \"same\"",
		"    Text 8..14 \" then \"",
		"    Strong 14..22 ",
		"      Text 16..20 \"same\"",
	}},
	{name: "t03_plain", opts: "default", src: "plain text", want: []string{
		"Root 0..10 ",
		"  Paragraph 0..10 ",
		"    Text 0..10 \"plain text\"",
	}},
	{name: "t09_escape_entity", opts: "default", src: "a\\* &amp; b", want: []string{
		"Root 0..11 ",
		"  Paragraph 0..11 ",
		"    Text 0..11 \"a* & b\"",
	}},
	{name: "t10_cross", opts: "default", src: "left **bold** right", want: []string{
		"Root 0..19 ",
		"  Paragraph 0..19 ",
		"    Text 0..5 \"left \"",
		"    Strong 5..13 ",
		"      Text 7..11 \"bold\"",
		"    Text 13..19 \" right\"",
	}},
	{name: "t11_code_in_strong", opts: "default", src: "**left `code` right**", want: []string{
		"Root 0..21 ",
		"  Paragraph 0..21 ",
		"    Strong 0..21 ",
		"      Text 2..7 \"left \"",
		"      InlineCode 7..13 \"code\"",
		"      Text 13..19 \" right\"",
	}},
	{name: "t12_code", opts: "default", src: "`code` x", want: []string{
		"Root 0..8 ",
		"  Paragraph 0..8 ",
		"    InlineCode 0..6 \"code\"",
		"    Text 6..8 \" x\"",
	}},
	{name: "t12_padded", opts: "default", src: "`` code ` value ``", want: []string{
		"Root 0..18 ",
		"  Paragraph 0..18 ",
		"    InlineCode 0..18 \"code ` value\"",
	}},
	{name: "t12_entity", opts: "default", src: "`&amp;`", want: []string{
		"Root 0..7 ",
		"  Paragraph 0..7 ",
		"    InlineCode 0..7 \"&amp;\"",
	}},
	{name: "t13_footnote", opts: "default", src: "before[^note] after\n\n[^note]: body", want: []string{
		"Root 0..34 ",
		"  Paragraph 0..19 ",
		"    Text 0..6 \"before\"",
		"    FootnoteReference 6..13 id=\"note\"",
		"    Text 13..19 \" after\"",
		"  FootnoteDefinition 21..34 id=\"note\"",
		"    Paragraph 30..34 ",
		"      Text 30..34 \"body\"",
	}},
	{name: "t14_mdx_text", opts: "mdx", src: "before {value + 1} after", want: []string{
		"Root 0..24 ",
		"  Paragraph 0..24 ",
		"    Text 0..7 \"before \"",
		"    MdxTextExpression 7..18 \"value + 1\"",
		"    Text 18..24 \" after\"",
	}},
	{name: "t15_mdx_flow", opts: "mdx", src: "{\n  value + 1\n}", want: []string{
		"Root 0..15 ",
		"  MdxFlowExpression 0..15 \"\\nvalue + 1\\n\"",
	}},
	{name: "t16_math_block", opts: "default", src: "$$\nx + y\n$$", want: []string{
		"Root 0..11 ",
		"  Math 0..11 \"x + y\"",
	}},
	{name: "t17_two_paras", opts: "default", src: "mapped\n\nA &amp; B", want: []string{
		"Root 0..17 ",
		"  Paragraph 0..6 ",
		"    Text 0..6 \"mapped\"",
		"  Paragraph 8..17 ",
		"    Text 8..17 \"A & B\"",
	}},
	{name: "t18_fence_info", opts: "default", src: "```rust\nrust\n```", want: []string{
		"Root 0..16 ",
		"  Code 0..16 lang=Some(\"rust\") \"rust\"",
	}},
	{name: "t19_close_candidate", opts: "default", src: "````text\n```\n````", want: []string{
		"Root 0..17 ",
		"  Code 0..17 lang=Some(\"text\") \"```\"",
	}},
	{name: "t20_indented", opts: "default", src: "    rust", want: []string{
		"Root 0..8 ",
		"  Code 0..8 lang=None \"rust\"",
	}},
	{name: "t21_indented_multi", opts: "default", src: "    one\n    two\n    three", want: []string{
		"Root 0..25 ",
		"  Code 0..25 lang=None \"one\\ntwo\\nthree\"",
	}},
	{name: "t22_fence_in_list", opts: "default", src: "- ```rust\n  one\n  two\n  ```", want: []string{
		"Root 0..27 ",
		"  List 0..27 ordered=false start=None spread=false",
		"    ListItem 0..27 checked=None spread=false",
		"      Code 2..27 lang=Some(\"rust\") \"one\\ntwo\"",
	}},
	{name: "t23_fence_in_quote", opts: "default", src: "> ```\n> one\n> two\n> ```", want: []string{
		"Root 0..23 ",
		"  Blockquote 0..23 ",
		"    Code 2..23 lang=None \"one\\ntwo\"",
	}},
	{name: "t24_fence_blank", opts: "default", src: "```\nsame\n\nsame\n```", want: []string{
		"Root 0..18 ",
		"  Code 0..18 lang=None \"same\\n\\nsame\"",
	}},
	{name: "t25_escape", opts: "default", src: "\\*", want: []string{
		"Root 0..2 ",
		"  Paragraph 0..2 ",
		"    Text 1..2 \"*\"",
	}},
	{name: "t26_esc_bs", opts: "default", src: "a\\\\b", want: []string{
		"Root 0..4 ",
		"  Paragraph 0..4 ",
		"    Text 0..4 \"a\\\\b\"",
	}},
	{name: "t26_esc_bs2", opts: "default", src: "\\\\\\\\b", want: []string{
		"Root 0..5 ",
		"  Paragraph 0..5 ",
		"    Text 1..5 \"\\\\\\\\b\"",
	}},
	{name: "t27_esc_math", opts: "default", src: "a\\\\$x$", want: []string{
		"Root 0..6 ",
		"  Paragraph 0..6 ",
		"    Text 0..3 \"a\\\\\"",
		"    InlineMath 3..6 \"x\"",
	}},
	{name: "t28_esc_hardbreak", opts: "default", src: "a\\\\  \nb", want: []string{
		"Root 0..7 ",
		"  Paragraph 0..7 ",
		"    Text 0..3 \"a\\\\\"",
		"    Break 3..6 ",
		"    Text 6..7 \"b\"",
	}},
	{name: "t29_trailing_img", opts: "default", src: "before ![alt](image.png)", want: []string{
		"Root 0..24 ",
		"  Paragraph 0..24 ",
		"    Text 0..7 \"before \"",
		"    Image 7..24 url=\"image.png\" alt=\"alt\" title=None",
	}},
	{name: "t30_leading_img", opts: "default", src: "![alt](image.png) after", want: []string{
		"Root 0..23 ",
		"  Paragraph 0..23 ",
		"    Image 0..17 url=\"image.png\" alt=\"alt\" title=None",
		"    Text 17..23 \" after\"",
	}},
	{name: "t31_enclosed_img", opts: "default", src: "before ![alt](image.png) after", want: []string{
		"Root 0..30 ",
		"  Paragraph 0..30 ",
		"    Text 0..7 \"before \"",
		"    Image 7..24 url=\"image.png\" alt=\"alt\" title=None",
		"    Text 24..30 \" after\"",
	}},
	{name: "t32_unreached_img", opts: "default", src: "before ![alt](image.png) after", want: []string{
		"Root 0..30 ",
		"  Paragraph 0..30 ",
		"    Text 0..7 \"before \"",
		"    Image 7..24 url=\"image.png\" alt=\"alt\" title=None",
		"    Text 24..30 \" after\"",
	}},
	{name: "t33_two_imgs", opts: "default", src: "![first](one.png)![second](two.png) after", want: []string{
		"Root 0..41 ",
		"  Paragraph 0..41 ",
		"    Image 0..17 url=\"one.png\" alt=\"first\" title=None",
		"    Image 17..35 url=\"two.png\" alt=\"second\" title=None",
		"    Text 35..41 \" after\"",
	}},
	{name: "t34_entity", opts: "default", src: "A &amp; B", want: []string{
		"Root 0..9 ",
		"  Paragraph 0..9 ",
		"    Text 0..9 \"A & B\"",
	}},
	{name: "t35_entities", opts: "default", src: "Copyright &copy; &#x1F600; &#169; 2024", want: []string{
		"Root 0..38 ",
		"  Paragraph 0..38 ",
		"    Text 0..38 \"Copyright © 😀 © 2024\"",
	}},
	{name: "t36_soft_indent", opts: "default", src: "a\n   b", want: []string{
		"Root 0..6 ",
		"  Paragraph 0..6 ",
		"    Text 0..6 \"a\\nb\"",
	}},
	{name: "t36_soft_quote", opts: "default", src: "> a\n> b", want: []string{
		"Root 0..7 ",
		"  Blockquote 0..7 ",
		"    Paragraph 2..7 ",
		"      Text 2..7 \"a\\nb\"",
	}},
	{name: "t36_soft_list", opts: "default", src: "- a\n  b", want: []string{
		"Root 0..7 ",
		"  List 0..7 ordered=false start=None spread=false",
		"    ListItem 0..7 checked=None spread=false",
		"      Paragraph 2..7 ",
		"        Text 2..7 \"a\\nb\"",
	}},
	{name: "t37_trailing_sp", opts: "default", src: "a \nb", want: []string{
		"Root 0..4 ",
		"  Paragraph 0..4 ",
		"    Text 0..4 \"a\\nb\"",
	}},
	{name: "t37_trailing_sp_crlf", opts: "default", src: "a \r\nb", want: []string{
		"Root 0..5 ",
		"  Paragraph 0..5 ",
		"    Text 0..5 \"a\\r\\nb\"",
	}},
	{name: "t37_crlf", opts: "default", src: "a\r\nb", want: []string{
		"Root 0..4 ",
		"  Paragraph 0..4 ",
		"    Text 0..4 \"a\\r\\nb\"",
	}},
	{name: "t38_nested_emph", opts: "default", src: "This has **_bold and italic_** text.", want: []string{
		"Root 0..36 ",
		"  Paragraph 0..36 ",
		"    Text 0..9 \"This has \"",
		"    Strong 9..30 ",
		"      Emphasis 11..28 ",
		"        Text 12..27 \"bold and italic\"",
		"    Text 30..36 \" text.\"",
	}},
	{name: "t39_html_img", opts: "default", src: "Before <img src=\"https://example.com/avatar.png\" alt=\"Avatar\" width=\"32\" height=\"32\" /> after.", want: []string{
		"Root 0..94 ",
		"  Paragraph 0..94 ",
		"    Text 0..7 \"Before \"",
		"    Html 7..87 \"<img src=\\\"https://example.com/avatar.png\\\" alt=\\\"Avatar\\\" width=\\\"32\\\" height=\\\"32\\\" />\"",
		"    Text 87..94 \" after.\"",
	}},
	{name: "t40_html_img2", opts: "default", src: "Before <img src=\"https://avatars.githubusercontent.com/u/5518\"> after.", want: []string{
		"Root 0..70 ",
		"  Paragraph 0..70 ",
		"    Text 0..7 \"Before \"",
		"    Html 7..63 \"<img src=\\\"https://avatars.githubusercontent.com/u/5518\\\">\"",
		"    Text 63..70 \" after.\"",
	}},
	{name: "t41_soft_lf", opts: "default", src: "this sentence\ncontinues as a soft wrap", want: []string{
		"Root 0..38 ",
		"  Paragraph 0..38 ",
		"    Text 0..38 \"this sentence\\ncontinues as a soft wrap\"",
	}},
	{name: "t41_soft_crlf", opts: "default", src: "this sentence\r\ncontinues as a soft wrap", want: []string{
		"Root 0..39 ",
		"  Paragraph 0..39 ",
		"    Text 0..39 \"this sentence\\r\\ncontinues as a soft wrap\"",
	}},
	{name: "t41_soft_cr", opts: "default", src: "this sentence\rcontinues as a soft wrap", want: []string{
		"Root 0..38 ",
		"  Paragraph 0..38 ",
		"    Text 0..38 \"this sentence\\rcontinues as a soft wrap\"",
	}},
	{name: "t42_soft_hard", opts: "default", src: "a\nb  \nc", want: []string{
		"Root 0..7 ",
		"  Paragraph 0..7 ",
		"    Text 0..3 \"a\\nb\"",
		"    Break 3..6 ",
		"    Text 6..7 \"c\"",
	}},
	{name: "t43_hard_spaces", opts: "default", src: "Owner: Jane  \nPersona: assistant", want: []string{
		"Root 0..32 ",
		"  Paragraph 0..32 ",
		"    Text 0..11 \"Owner: Jane\"",
		"    Break 11..14 ",
		"    Text 14..32 \"Persona: assistant\"",
	}},
	{name: "t43_hard_bs", opts: "default", src: "Owner: Jane\\\nPersona: assistant", want: []string{
		"Root 0..31 ",
		"  Paragraph 0..31 ",
		"    Text 0..11 \"Owner: Jane\"",
		"    Break 11..13 ",
		"    Text 13..31 \"Persona: assistant\"",
	}},
	{name: "t44_fm_off", opts: "default", src: "---\nSome text\n---", want: []string{
		"Root 0..17 ",
		"  ThematicBreak 0..3 ",
		"  Heading 4..17 depth=2",
		"    Text 4..13 \"Some text\"",
	}},
	{name: "t45_fm_on", opts: "fm", src: "---\n- name: example\n---", want: []string{
		"Root 0..23 ",
		"  Yaml 0..23 \"- name: example\"",
	}},
	{name: "t46_fenced_yaml", opts: "default", src: "```yaml\nname: example\n```", want: []string{
		"Root 0..25 ",
		"  Code 0..25 lang=Some(\"yaml\") \"name: example\"",
	}},
	{name: "t47_ticker", opts: "default", src: "$TSLA.US", want: []string{
		"Root 0..8 ",
		"  Paragraph 0..8 ",
		"    Text 0..8 \"$TSLA.US\"",
	}},
	{name: "t48_math_code_esc", opts: "default", src: "$x$ `$code$` \\$escaped\\$", want: []string{
		"Root 0..24 ",
		"  Paragraph 0..24 ",
		"    InlineMath 0..3 \"x\"",
		"    Text 3..4 \" \"",
		"    InlineCode 4..12 \"$code$\"",
		"    Text 12..24 \" $escaped$\"",
	}},
	{name: "t49_math_in_strong", opts: "default", src: "中文 **before $x^2$ after** and `$ignored$`", want: []string{
		"Root 0..45 ",
		"  Paragraph 0..45 ",
		"    Text 0..7 \"中文 \"",
		"    Strong 7..29 ",
		"      Text 9..16 \"before \"",
		"      InlineMath 16..21 \"x^2\"",
		"      Text 21..27 \" after\"",
		"    Text 29..34 \" and \"",
		"    InlineCode 34..45 \"$ignored$\"",
	}},
	{name: "t50_math_block", opts: "default", src: "$$\n\\sum_{i=1}^{n} i\n$$", want: []string{
		"Root 0..22 ",
		"  Math 0..22 \"\\\\sum_{i=1}^{n} i\"",
	}},
	{name: "t51_unclaimed", opts: "default", src: "EPS of <strong>$1.56</strong> beat the <strong>$1.50</strong> consensus, *up $2*", want: []string{
		"Root 0..80 ",
		"  Paragraph 0..80 ",
		"    Text 0..7 \"EPS of \"",
		"    Html 7..15 \"<strong>\"",
		"    InlineMath 15..48 \"1.56</strong> beat the <strong>\"",
		"    Text 48..52 \"1.50\"",
		"    Html 52..61 \"</strong>\"",
		"    Text 61..73 \" consensus, \"",
		"    Emphasis 73..80 ",
		"      Text 74..79 \"up $2\"",
	}},
	{name: "t52_claimed", opts: "default", src: "area $x^2$ costs $5 and $10", want: []string{
		"Root 0..27 ",
		"  Paragraph 0..27 ",
		"    Text 0..5 \"area \"",
		"    InlineMath 5..10 \"x^2\"",
		"    Text 10..17 \" costs \"",
		"    InlineMath 17..25 \"5 and \"",
		"    Text 25..27 \"10\"",
	}},
	{name: "t53_html_pairs", opts: "default", src: "a <strong>b *c* <em>d</em></strong> e <b>f</b> <i>g</i> <del>h</del> <br> <strong>unclosed", want: []string{
		"Root 0..90 ",
		"  Paragraph 0..90 ",
		"    Text 0..2 \"a \"",
		"    Html 2..10 \"<strong>\"",
		"    Text 10..12 \"b \"",
		"    Emphasis 12..15 ",
		"      Text 13..14 \"c\"",
		"    Text 15..16 \" \"",
		"    Html 16..20 \"<em>\"",
		"    Text 20..21 \"d\"",
		"    Html 21..26 \"</em>\"",
		"    Html 26..35 \"</strong>\"",
		"    Text 35..38 \" e \"",
		"    Html 38..41 \"<b>\"",
		"    Text 41..42 \"f\"",
		"    Html 42..46 \"</b>\"",
		"    Text 46..47 \" \"",
		"    Html 47..50 \"<i>\"",
		"    Text 50..51 \"g\"",
		"    Html 51..55 \"</i>\"",
		"    Text 55..56 \" \"",
		"    Html 56..61 \"<del>\"",
		"    Text 61..62 \"h\"",
		"    Html 62..68 \"</del>\"",
		"    Text 68..69 \" \"",
		"    Html 69..73 \"<br>\"",
		"    Text 73..74 \" \"",
		"    Html 74..82 \"<strong>\"",
		"    Text 82..90 \"unclosed\"",
	}},
	{name: "t54_dollars", opts: "default", src: "spent $5 and $10 today", want: []string{
		"Root 0..22 ",
		"  Paragraph 0..22 ",
		"    Text 0..6 \"spent \"",
		"    InlineMath 6..14 \"5 and \"",
		"    Text 14..22 \"10 today\"",
	}},
	{name: "t55_rt1", opts: "default", src: "*before $x$ after*", want: []string{
		"Root 0..18 ",
		"  Paragraph 0..18 ",
		"    Emphasis 0..18 ",
		"      Text 1..8 \"before \"",
		"      InlineMath 8..11 \"x\"",
		"      Text 11..17 \" after\"",
	}},
	{name: "t55_rt2", opts: "default", src: "*before **$x$** after*", want: []string{
		"Root 0..22 ",
		"  Paragraph 0..22 ",
		"    Emphasis 0..22 ",
		"      Text 1..8 \"before \"",
		"      Strong 8..15 ",
		"        InlineMath 10..13 \"x\"",
		"      Text 15..21 \" after\"",
	}},
	{name: "t55_rt3", opts: "default", src: "**before *$x$* after**", want: []string{
		"Root 0..22 ",
		"  Paragraph 0..22 ",
		"    Strong 0..22 ",
		"      Text 2..9 \"before \"",
		"      Emphasis 9..14 ",
		"        InlineMath 10..13 \"x\"",
		"      Text 14..20 \" after\"",
	}},
	{name: "t56_cjk_code", opts: "default", src: "`原子`", want: []string{
		"Root 0..8 ",
		"  Paragraph 0..8 ",
		"    InlineCode 0..8 \"原子\"",
	}},
	{name: "t57_ticker", opts: "default", src: "$TSLA.US", want: []string{
		"Root 0..8 ",
		"  Paragraph 0..8 ",
		"    Text 0..8 \"$TSLA.US\"",
	}},
	{name: "x_table", opts: "default", src: "| a | b |\n|:--|--:|\n| *c* | `d` |\n| e |", want: []string{
		"Root 0..39 ",
		"  Table 0..39 align=[Left, Right]",
		"    TableRow 0..9 ",
		"      TableCell 0..4 ",
		"        Text 2..3 \"a\"",
		"      TableCell 4..9 ",
		"        Text 6..7 \"b\"",
		"    TableRow 20..33 ",
		"      TableCell 20..26 ",
		"        Emphasis 22..25 ",
		"          Text 23..24 \"c\"",
		"      TableCell 26..33 ",
		"        InlineCode 28..31 \"d\"",
		"    TableRow 34..39 ",
		"      TableCell 34..39 ",
		"        Text 36..37 \"e\"",
	}},
	{name: "x_tasklist", opts: "default", src: "- [x] done\n- [ ] todo\n\n  para", want: []string{
		"Root 0..29 ",
		"  List 0..29 ordered=false start=None spread=false",
		"    ListItem 0..10 checked=Some(true) spread=false",
		"      Paragraph 6..10 ",
		"        Text 6..10 \"done\"",
		"    ListItem 11..29 checked=Some(false) spread=true",
		"      Paragraph 17..21 ",
		"        Text 17..21 \"todo\"",
		"      Paragraph 25..29 ",
		"        Text 25..29 \"para\"",
	}},
	{name: "x_strike", opts: "default", src: "a ~~del~~ b ~one~ c", want: []string{
		"Root 0..19 ",
		"  Paragraph 0..19 ",
		"    Text 0..2 \"a \"",
		"    Delete 2..9 ",
		"      Text 4..7 \"del\"",
		"    Text 9..12 \" b \"",
		"    Delete 12..17 ",
		"      Text 13..16 \"one\"",
		"    Text 17..19 \" c\"",
	}},
	{name: "x_autolink", opts: "default", src: "see https://example.com/x and www.example.org, <http://a.b> and me@x.io", want: []string{
		"Root 0..71 ",
		"  Paragraph 0..71 ",
		"    Text 0..4 \"see \"",
		"    Link 4..25 url=\"https://example.com/x\" title=None",
		"      Text 4..25 \"https://example.com/x\"",
		"    Text 25..30 \" and \"",
		"    Link 30..45 url=\"http://www.example.org\" title=None",
		"      Text 30..45 \"www.example.org\"",
		"    Text 45..47 \", \"",
		"    Link 47..59 url=\"http://a.b\" title=None",
		"      Text 48..58 \"http://a.b\"",
		"    Text 59..64 \" and \"",
		"    Link 64..71 url=\"mailto:me@x.io\" title=None",
		"      Text 64..71 \"me@x.io\"",
	}},
	{name: "x_reflinks", opts: "default", src: "[full][Ref] [collapsed][] [ref]\n\n[ref]: /url \"T\"", want: []string{
		"Root 0..48 ",
		"  Paragraph 0..31 ",
		"    LinkReference 0..11 id=\"ref\"",
		"      Text 1..5 \"full\"",
		"    Text 11..26 \" [collapsed][] \"",
		"    LinkReference 26..31 id=\"ref\"",
		"      Text 27..30 \"ref\"",
		"  Definition 33..48 id=\"ref\" url=\"/url\" title=Some(\"T\")",
	}},
	{name: "x_link", opts: "default", src: "[a **b**](http://x.y \"t\") and ![i](p.png)", want: []string{
		"Root 0..41 ",
		"  Paragraph 0..41 ",
		"    Link 0..25 url=\"http://x.y\" title=Some(\"t\")",
		"      Text 1..3 \"a \"",
		"      Strong 3..8 ",
		"        Text 5..6 \"b\"",
		"    Text 25..30 \" and \"",
		"    Image 30..41 url=\"p.png\" alt=\"i\" title=None",
	}},
	{name: "x_html_block", opts: "default", src: "<div>\n*x*\n</div>\n\npara", want: []string{
		"Root 0..22 ",
		"  Html 0..16 \"<div>\\n*x*\\n</div>\"",
		"  Paragraph 18..22 ",
		"    Text 18..22 \"para\"",
	}},
	{name: "x_headings", opts: "default", src: "# H1 #\n\nSetext\n===\n\n***", want: []string{
		"Root 0..23 ",
		"  Heading 0..6 depth=1",
		"    Text 2..4 \"H1\"",
		"  Heading 8..18 depth=1",
		"    Text 8..14 \"Setext\"",
		"  ThematicBreak 20..23 ",
	}},
	{name: "x_ordered", opts: "default", src: "3. a\n4. b\n\n   c", want: []string{
		"Root 0..15 ",
		"  List 0..15 ordered=true start=Some(3) spread=false",
		"    ListItem 0..4 checked=None spread=false",
		"      Paragraph 3..4 ",
		"        Text 3..4 \"a\"",
		"    ListItem 5..15 checked=None spread=true",
		"      Paragraph 8..9 ",
		"        Text 8..9 \"b\"",
		"      Paragraph 14..15 ",
		"        Text 14..15 \"c\"",
	}},
	{name: "x_fm_yaml_ok", opts: "fm", src: "---\ntitle: x\n---\n# Hi", want: []string{
		"Root 0..21 ",
		"  Yaml 0..16 \"title: x\"",
		"  Heading 17..21 depth=1",
		"    Text 19..21 \"Hi\"",
	}},
	{name: "x_fm_toml", opts: "fm", src: "+++\ntitle = 1\n+++\nbody", want: []string{
		"Root 0..22 ",
		"  Toml 0..17 \"title = 1\"",
		"  Paragraph 18..22 ",
		"    Text 18..22 \"body\"",
	}},
	{name: "x_unclosed_fence", opts: "default", src: "```\nopen", want: []string{
		"Root 0..8 ",
		"  Code 0..8 lang=None \"open\"",
	}},
	{name: "x_math_multiline", opts: "default", src: "a $x\ny$ b $$z$$", want: []string{
		"Root 0..15 ",
		"  Paragraph 0..15 ",
		"    Text 0..2 \"a \"",
		"    InlineMath 2..7 \"x\\ny\"",
		"    Text 7..10 \" b \"",
		"    InlineMath 10..15 \"z\"",
	}},
	{name: "x_footnote_multi", opts: "default", src: "x[^a]\n\n[^a]: one\n    two\n\nafter", want: []string{
		"Root 0..31 ",
		"  Paragraph 0..5 ",
		"    Text 0..1 \"x\"",
		"    FootnoteReference 1..5 id=\"a\"",
		"  FootnoteDefinition 7..25 id=\"a\"",
		"    Paragraph 13..24 ",
		"      Text 13..24 \"one\\ntwo\"",
		"  Paragraph 26..31 ",
		"    Text 26..31 \"after\"",
	}},
	{name: "x_nested", opts: "default", src: "> - a\n>   - **b** [c](d)\n> e", want: []string{
		"Root 0..28 ",
		"  Blockquote 0..28 ",
		"    List 2..28 ordered=false start=None spread=false",
		"      ListItem 2..28 checked=None spread=false",
		"        Paragraph 4..5 ",
		"          Text 4..5 \"a\"",
		"        List 10..28 ordered=false start=None spread=false",
		"          ListItem 10..28 checked=None spread=false",
		"            Paragraph 12..28 ",
		"              Strong 12..17 ",
		"                Text 14..15 \"b\"",
		"              Text 17..18 \" \"",
		"              Link 18..24 url=\"d\" title=None",
		"                Text 19..20 \"c\"",
		"              Text 24..28 \"\\ne\"",
	}},
	{name: "x_crlf_doc", opts: "default", src: "# T\r\n\r\npara\r\nline\r\n", want: []string{
		"Root 0..19 ",
		"  Heading 0..3 depth=1",
		"    Text 2..3 \"T\"",
		"  Paragraph 7..17 ",
		"    Text 7..17 \"para\\r\\nline\"",
	}},
}

func mdastOracleOptions(opts string) mdastParseOptions {
	o := mdastParseOptions{mathText: opts != "nomath", mathFlow: opts != "nomath"}
	o.frontmatter = opts == "fm"
	o.mdx = opts == "mdx"
	return o
}

// TestMdastMatchesMarkdownRsOracle is the differential golden test: every
// node's kind, nesting, start and end offsets and values match the
// recorded markdown-rs tree.
func TestMdastMatchesMarkdownRsOracle(t *testing.T) {
	for _, c := range mdastOracleCases {
		root, err := mdastParse(c.src, mdastOracleOptions(c.opts))
		if c.opts == "mdx" {
			if err != errMdastMDXUnsupported {
				t.Errorf("%s: MDX parse error = %v, want errMdastMDXUnsupported", c.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got := mdastDebugLines(root)
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s (%q):\n got:\n%s\nwant:\n%s", c.name, c.src, strings.Join(got, "\n"), strings.Join(c.want, "\n"))
		}
	}
}
