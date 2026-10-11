import sys

def rep_file(p, pairs):
    s = open(p, encoding='utf-8', newline='').read()
    for old, new in pairs:
        assert s.count(old) == 1, f"{p}: needle count {s.count(old)} != 1: {old[:70]!r}"
        s = s.replace(old, new)
    open(p, 'w', encoding='utf-8', newline='').write(s)
    print(p, 'updated')

# 1) abi.go: revision 10
rep_file('internal/native/abi.go', [
    ('''	// nativeRevision mirrors GPUI_GO_NATIVE_REVISION. Ticket02 shipped
''', '''	// nativeRevision mirrors GPUI_GO_NATIVE_REVISION. Ticket02 shipped
'''),
])

# locate the exact revision block in abi.go first
s = open('internal/native/abi.go', encoding='utf-8', newline='').read()
i = s.find('nativeRevision uint32 = 9')
assert i > 0
s = s.replace('nativeRevision uint32 = 9', 'nativeRevision uint32 = 10')
# extend the comment above it
old_comment = '''	// service in reserved slot 7) bumped it to 8; ticket19 (the SVG
	// service in reserved slot 8)'''
assert s.count(old_comment) == 1, "abi comment"
s = s.replace(old_comment, '''	// service in reserved slot 7) bumped it to 8; ticket19 (the SVG
	// service in reserved slot 8) bumped it to 9; the wave-4 rebuild
	// (tickets 22/28/29/32: the a11y provider service in slot 9, the
	// capture service in slot 10, the renderer service v3 device-loss
	// and render-to-image entries, the AtlasKey::Svg variant)''')
open('internal/native/abi.go', 'w', encoding='utf-8', newline='').write(s)
print('abi.go revision 10')

# 2) capability mask assertions: 0x3FF -> 0xFFF everywhere cumulative
for p in ['internal/native/glyph_test.go', 'internal/glyphspec/glyph_spec_test.go', 'internal/scenespec/scene_spec_test.go']:
    s = open(p, encoding='utf-8', newline='').read()
    n = s.count('0x3FF')
    s = s.replace('0x3FF', '0xFFF')
    # also fix comments mentioning "mask 0x3FF"
    s = s.replace('the cumulative capability mask 0x3FF', 'the cumulative capability mask 0xFFF')
    open(p, 'w', encoding='utf-8', newline='').write(s)
    print(p, 'mask updated', n)

# 3) consumerspec evidence constants
rep_file('internal/consumerspec/evidence.go', [
    ('''	// EvidenceArtifactSHA256 is the recorded SHA-256 of the DLL.
	EvidenceArtifactSHA256 = "f10c79737a4f68f629efa70533b016ccdcfb03b919e0d8a2058b696d062fde8c"
	// EvidenceArtifactBytes is the recorded DLL size.
	EvidenceArtifactBytes = 18216448
	// EvidenceNativeRevision is the recorded native bridge revision.
	EvidenceNativeRevision = 9''',
'''	// EvidenceArtifactSHA256 is the recorded SHA-256 of the DLL.
	EvidenceArtifactSHA256 = "6442f00acb8064a91a6d8aabb3996d47b1a93d4f7745df616fd3cbb1782258ce"
	// EvidenceArtifactBytes is the recorded DLL size.
	EvidenceArtifactBytes = 18860544
	// EvidenceNativeRevision is the recorded native bridge revision.
	EvidenceNativeRevision = 10'''),
    ('''// The artifact identity recorded for the current native build (native
// revision 7). These constants are the evidence values the delivery checks
// compare the module's embedded artifact against; they cite:
//
//   - evidence/ticket16-paths-filters.json (the most recent recorded
//     identity: "12,489,728 bytes; sha256 d648523b…; static CRT
//     (OS-only imports)", native revision 7), and
//   - evidence/ticket13-focus-keyboard.json (same artifact, revision 7).
//
// When the maintainer artifact rotates, these constants, the embedded
// manifest pair and the evidence records move together: the tests fail
// loudly instead of accepting a silent identity drift.''',
'''// The artifact identity recorded for the current native build (native
// revision 10, the wave-4 rebuild). These constants are the evidence
// values the delivery checks compare the module's embedded artifact
// against; they cite:
//
//   - evidence/ticket22-uia-provider.json, evidence/ticket28-capture.json,
//     evidence/ticket29-device-loss.json and
//     evidence/ticket32-render-to-image.json (the wave-4 evidence
//     recording: "18,860,544 bytes; sha256 6442f00a…; static CRT
//     (OS-only imports)", native revision 10, capability mask 0xFFF).
//
// When the maintainer artifact rotates, these constants, the embedded
// manifest pair and the evidence records move together: the tests fail
// loudly instead of accepting a silent identity drift.'''),
])
