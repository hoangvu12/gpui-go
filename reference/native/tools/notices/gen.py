"""Regenerate THIRD_PARTY_NOTICES.md for the embedded native DLL.

Usage (from the repository root, with the pinned CE checkout at
reference/ce-source and the crates already fetched):

    cargo metadata --format-version 1 --manifest-path reference/native/Cargo.toml         --filter-platform x86_64-pc-windows-msvc --offline > meta.json
    python -I reference/native/tools/notices/gen.py meta.json THIRD_PARTY_NOTICES.md         internal/native/artifacts/manifest.json

cargo metadata resolves features across every target and dev graph, so it
lists a few crates the DLL never links. The artifact manifest records the
cargo tree of the actual build; the crate set is the intersection of the
metadata's normal-dependency closure with that tree (which drops the
build-script-only crates such as cc and embed-resource).

The Go sources' own third-party material (the Tailwind CSS palette) is not
in the cargo graph; GO_SOURCES below is written verbatim between the crate
table and the license texts. Edit it here, not in the generated file.
"""
import json, os, re, sys, collections, hashlib

GO_SOURCES = '''
## Go sources

The Go packages of this module also include material from the projects below, which is not part of the native library. Each license text follows its entry.

### Tailwind CSS 3.4.17

Source: https://github.com/tailwindlabs/tailwindcss (v3.4.17), MIT License.

Covers the default color palette values in `cmd/tailwindgen/palette.go`, `gpui/tailwind_palette_generated.go` and `tw/palette_generated.go`, and the utility class names and scales that `Styled.Class` and the Tailwind-named `Styled` methods follow.

```text
MIT License

Copyright (c) Tailwind Labs, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### clipperhouse/uax29 2.7.0 (Go module)

Source: https://github.com/clipperhouse/uax29 (module `github.com/clipperhouse/uax29/v2` v2.7.0), MIT License. A module requirement of `base/segment` (Unicode word and grapheme segmentation).

```text
MIT License

Copyright (c) 2020 Matt Sherman

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### yuin/goldmark 1.8.6 (Go module)

Source: https://github.com/yuin/goldmark (module `github.com/yuin/goldmark` v1.8.6), MIT License. A module requirement of `base` (the Markdown parser behind `base/text_mdast.go`, which adapts its tree to mdast).

```text
MIT License

Copyright (c) 2019 Yusuke Inuzuka

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### golang.org/x/net 0.61.0 (Go module)

Source: https://cs.opensource.google/go/x/net (module `golang.org/x/net` v0.61.0), BSD 3-Clause License. A module requirement of `base` (its `html` package parses the HTML that `base/text_html.go` and `base/text_html5minify.go` turn into document nodes).

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### gpui-component (GPUI Base) c0820693

Source: https://github.com/gpui-ce/gpui-component (commit `c08206932417f863062d2ab70cc2854c6ca04238`, `crates/base`), Apache License 2.0, Copyright 2024 - 2026 Longbridge <https://longbridge.com>. The `base` package and its subpackages are a Go port of that crate, and `base/testdata/dock` holds copies of its dock fixtures. The Apache License 2.0 text is this module's own `LICENSE` file.

### zed-sum-tree 0.2.0

Source: https://crates.io/crates/zed-sum-tree (0.2.0), Apache License 2.0, Copyright 2022 - 2025 Zed Industries, Inc. `base/sumtree` is a Go port of its `sum_tree.rs` and `cursor.rs`. The Apache License 2.0 text is this module's own `LICENSE` file.

### ropey 2.0.0-beta.1

Source: https://github.com/cessen/ropey (crate 2.0.0-beta.1), MIT OR Apache-2.0, used here under MIT. `base/rope` recreates the ropey API subset GPUI Base uses, following its sources and tests.

```text
MIT License

Copyright (c) 2023 Nathan Vegdahl

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### data-url 0.3.2, chrono 0.4.45 and unicode-segmentation 1.13.3

`base/dataurl` is a Go port of the data-url crate (https://github.com/servo/rust-url), `base/strftime` follows chrono's strftime parser and formatter (https://github.com/chronotope/chrono), and `base/segment/testdata/unicode-segmentation-1.13.3.jsonl` records output of the unicode-segmentation crate (https://github.com/unicode-rs/unicode-segmentation). All three are MIT OR Apache-2.0; their license texts are printed under "License texts" below (data-url 0.3.2, chrono 0.4.45, unicode-segmentation 1.13.3).

### web-platform-tests data

`base/dataurl/testdata/{data-urls,base64,mime-types,generated-mime-types}.json` are web-platform-tests vectors (https://github.com/web-platform-tests/wpt), as redistributed in the data-url 0.3.2 crate's `tests/` directory. 3-Clause BSD License.

```text
Copyright 2019 web-platform-tests contributors

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### Unicode Character Database test data

`base/segment/testdata/unicode-segmentation-1.13.3.jsonl` includes the strings of Unicode 17.0.0 `WordBreakTest.txt` and `GraphemeBreakTest.txt` (https://www.unicode.org/Public/17.0.0/ucd/auxiliary/), under the Unicode License V3, printed below under "icu_collections 2.3.0 and 21 more".
'''

meta = json.load(open(sys.argv[1], encoding='utf-8'))
out = sys.argv[2]
pk = {p['id']: p for p in meta['packages']}
nodes = {n['id']: n for n in meta['resolve']['nodes']}
root = [p['id'] for p in meta['packages'] if p['name'] == 'gpui-go-native'][0]
seen, stack = set(), [root]
while stack:
    i = stack.pop()
    if i in seen: continue
    seen.add(i)
    for d in nodes[i]['deps']:
        if any(k['kind'] is None for k in d['dep_kinds']):
            stack.append(d['pkg'])
seen.discard(root)
built = set(re.findall(r'[─ ]([A-Za-z0-9_-]+) v([0-9]+\.[0-9]+\.[0-9]+[-+.A-Za-z0-9]*)', open(sys.argv[3], encoding='utf-8').read()))
seen = {i for i in seen if (pk[i]['name'], pk[i]['version']) in built}
pkgs = sorted((pk[i] for i in seen), key=lambda p: (p['name'], p['version']))
texts = collections.OrderedDict()  # hash -> (text, [crates])
rows = []
missing = []
for p in pkgs:
    d = os.path.dirname(p['manifest_path'])
    files = sorted(f for f in os.listdir(d) if f.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE', 'COPYRIGHT')) and os.path.isfile(os.path.join(d, f)))
    base = d
    if not files and (p.get('source') is None or p['source'].startswith('git+')):
        up = d
        for _ in range(4):
            up = os.path.dirname(up)
            cand = sorted(f for f in os.listdir(up) if f.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE')) and os.path.isfile(os.path.join(up, f)))
            if cand:
                files, base = cand, up
                break
    if not p.get('license') and not p.get('license_file') and base != d:
        p['license'] = 'not declared; repository license below'
    if p.get('license_file'):
        lf = os.path.normpath(os.path.join(d, p['license_file']))
        if os.path.isfile(lf) and os.path.basename(lf) not in files:
            files.append(os.path.relpath(lf, d))
    lic = p.get('license') or ('see ' + p['license_file'] if p.get('license_file') else 'not declared')
    src = 'path' if p.get('source') is None else 'crates.io' if 'crates.io' in p['source'] else p['source']
    rows.append((p['name'], p['version'], lic, p.get('repository') or ''))
    if not files: missing.append(f"{p['name']} {p['version']}")
    for f in files:
        t = open(os.path.join(base, f), encoding='utf-8', errors='replace').read().replace('\r\n', '\n').strip()
        h = hashlib.sha256(t.encode()).hexdigest()
        texts.setdefault(h, [t, []])[1].append(f"{p['name']} {p['version']} ({f})")
with open(out, 'w', encoding='utf-8', newline='\n') as o:
    o.write("# Third-party notices\n\n")
    o.write("The embedded native library `internal/native/artifacts/gpui_go_native.dll` is compiled from the crate `gpui-go-native` "
            "(`reference/native`) and statically links the Rust crates listed below, including the pinned GPUI-CE sources "
            "(commit `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`). The list is the crate's normal (non-build, non-dev) dependency "
            "graph for `x86_64-pc-windows-msvc` from `cargo metadata`, restricted to the crates in the cargo tree the artifact manifest records for the actual build. License texts follow the table, "
            "each printed once with the crates that ship it.\n\n")
    o.write(f"{len(rows)} crates.\n\n| Crate | Version | License | Repository |\n|---|---|---|---|\n")
    for r in rows:
        o.write("| " + " | ".join(x.replace('|', '/') for x in r) + " |\n")
    if missing:
        o.write("\nThese crates ship no license file in their published source; their declared license is in the table above: " + ", ".join(missing) + ".\n")
    o.write(GO_SOURCES)
    o.write("\n## License texts\n")
    for h, (t, who) in texts.items():
        o.write("\n### " + who[0].split(' (')[0] + (f" and {len(who)-1} more" if len(who) > 1 else "") + "\n\n")
        o.write("Used by: " + ", ".join(who) + "\n\n```text\n" + t + "\n```\n")
print(len(rows), "crates;", len(texts), "distinct texts; missing:", len(missing))
