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
"""
import json, os, re, sys, collections, hashlib
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
    o.write("\n## License texts\n")
    for h, (t, who) in texts.items():
        o.write("\n### " + who[0].split(' (')[0] + (f" and {len(who)-1} more" if len(who) > 1 else "") + "\n\n")
        o.write("Used by: " + ", ".join(who) + "\n\n```text\n" + t + "\n```\n")
print(len(rows), "crates;", len(texts), "distinct texts; missing:", len(missing))
