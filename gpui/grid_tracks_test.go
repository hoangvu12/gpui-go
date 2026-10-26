package gpui

import (
	"strings"
	"testing"
)

// The browser-core ticket 42 style-surface gates: the track-list and
// area-template parsing, the builder surface, and the Tailwind classes
// (grid-cols-[240px_1fr], grid-rows-[auto_auto],
// grid-areas-["head_head"_"side_main"], grid-area-[head]).

func TestParseTrackListText(t *testing.T) {
	tracks, err := parseTrackListText("240px 1fr")
	if err != nil {
		t.Fatalf("240px 1fr: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}
	if tracks[0].Min.Kind != TrackLength || tracks[0].Min.Length != 240 ||
		tracks[0].Max.Kind != TrackLength || tracks[0].Max.Length != 240 {
		t.Fatalf("track 1 = %+v, want the fixed 240px minmax pair", tracks[0])
	}
	if tracks[1].Min.Kind != TrackAuto || tracks[1].Max.Kind != TrackFr || tracks[1].Max.Length != 1 {
		t.Fatalf("track 2 = %+v, want minmax(auto, 1fr)", tracks[1])
	}

	for _, bad := range []string{
		"",
		"-10px 1fr",         // negative length
		"240px, 1fr",        // commas separate, not join
		"minmax(10px)",      // a one-sided minmax
		"minmax(10px 1fr)",  // missing comma
		"minmax(1fr, 10px)", // an fr minimum
		"0fr",               // a non-positive factor
		"240px stretch",     // unknown keyword
		"minmax(10px, 1fr",  // unclosed
	} {
		if _, err := parseTrackListText(bad); err == nil {
			t.Errorf("parseTrackListText(%q) succeeded, want an error", bad)
		}
	}

	// minmax and the content keywords round-trip.
	tracks, err = parseTrackListText("minmax(10px, 1fr) auto min-content max-content")
	if err != nil {
		t.Fatalf("minmax form: %v", err)
	}
	if len(tracks) != 4 {
		t.Fatalf("got %d tracks, want 4", len(tracks))
	}
	if tracks[0].Min.Kind != TrackLength || tracks[0].Min.Length != 10 || tracks[0].Max.Kind != TrackFr {
		t.Fatalf("minmax track = %+v, want minmax(10px, 1fr)", tracks[0])
	}
	if tracks[1].Min.Kind != TrackAuto || tracks[1].Max.Kind != TrackAuto {
		t.Fatalf("auto track = %+v, want minmax(auto, auto)", tracks[1])
	}
	if tracks[2].Min.Kind != TrackMinContent {
		t.Fatalf("min-content track = %+v", tracks[2])
	}
	if tracks[3].Max.Kind != TrackMaxContent {
		t.Fatalf("max-content track = %+v", tracks[3])
	}
}

func TestParseGridTemplateAreas(t *testing.T) {
	template, err := ParseGridTemplateAreas(`"head head" "side main"`)
	if err != nil {
		t.Fatalf("two rows: %v", err)
	}
	if len(template.Rows) != 2 || len(template.Rows[0]) != 2 {
		t.Fatalf("rows = %v, want 2 rows of 2 cells", template.Rows)
	}
	names := template.AreaNames()
	if strings.Join(names, ",") != "head,side,main" {
		t.Fatalf("area names = %v, want [head side main]", names)
	}
	rect := template.rects["head"]
	if rect.rowStart != 1 || rect.rowEnd != 2 || rect.columnStart != 1 || rect.columnEnd != 3 {
		t.Fatalf("head rect = %+v, want 1..2 x 1..3 (spans both columns)", rect)
	}
	rect = template.rects["main"]
	if rect.rowStart != 2 || rect.rowEnd != 3 || rect.columnStart != 2 || rect.columnEnd != 3 {
		t.Fatalf("main rect = %+v, want 2..3 x 2..3", rect)
	}

	// A spanning area and the null cell.
	template, err = ParseGridTemplateAreas(`"a a ." "a a ."`)
	if err != nil {
		t.Fatalf("spanning area: %v", err)
	}
	if rect := template.rects["a"]; rect.rowStart != 1 || rect.rowEnd != 3 || rect.columnStart != 1 || rect.columnEnd != 3 {
		t.Fatalf("a rect = %+v, want 1..3 x 1..3 (spans both rows and columns)", rect)
	}
	// An L-shaped area is invalid (the name must form one rectangle).
	if _, err := ParseGridTemplateAreas(`"a a" "a b"`); err == nil {
		t.Fatal("an L-shaped area must be an error")
	}

	// The null cell keeps its own column.
	template, err = ParseGridTemplateAreas(`". top" "bottom ."`)
	if err != nil {
		t.Fatalf("null cells: %v", err)
	}
	if len(template.AreaNames()) != 2 {
		t.Fatalf("names = %v, want top and bottom only", template.AreaNames())
	}
	if rect := template.rects["top"]; rect.columnStart != 2 {
		t.Fatalf("top rect = %+v, want column 2", rect)
	}

	// Non-rectangular, ragged and malformed templates are errors.
	for _, bad := range []string{
		"",
		`"a b" "c"`,   // ragged
		`"a b" "c a"`, // non-rectangular a
		`"a a" "a a"`, // duplicate rectangle (fine shape, one name)
		`a b`,         // unquoted
		`"a b`,        // unterminated
		`"1a b"`,      // a digit-led name
		`"none x"`,    // a CSS-wide keyword
		`""`,          // an empty row
	} {
		if _, err := ParseGridTemplateAreas(bad); err == nil {
			// "a a / a a" is rectangular and legal; only the listed
			// shapes fail. Adjust: that entry is legal, so skip it.
			if bad == `"a a" "a a"` {
				continue
			}
			t.Errorf("ParseGridTemplateAreas(%q) succeeded, want an error", bad)
		}
	}
}

func TestGridStyleBuildersAndClasses(t *testing.T) {
	// The builder surface writes the fields the adapter forwards.
	styled := Styles().
		GridColsList(PxTrack(240), FrTrack(1)).
		GridRowsList(AutoTrack()).
		GridTemplateAreas("head head", "side main").
		GridArea("head")
	refinement := styled.Refinement()
	if tracks, ok := refinement.GridColsList.Get(); !ok || len(tracks) != 2 {
		t.Fatalf("GridColsList refinement = %v", refinement.GridColsList)
	}
	if tracks, ok := refinement.GridRowsList.Get(); !ok || len(tracks) != 1 || tracks[0].Min.Kind != TrackAuto {
		t.Fatalf("GridRowsList refinement = %v", refinement.GridRowsList)
	}
	if template, ok := refinement.GridAreas.Get(); !ok || template.AreaNames()[0] != "head" {
		t.Fatalf("GridAreas refinement = %v", refinement.GridAreas)
	}
	if name, ok := refinement.GridAreaName.Get(); !ok || name != "head" {
		t.Fatalf("GridAreaName refinement = %v", refinement.GridAreaName)
	}

	// The classes parse to the same fields.
	div := Div().Class("grid-cols-[240px_1fr]")
	if tracks, ok := div.StyleMut().GridColsList.Get(); !ok || len(tracks) != 2 || tracks[0].Min.Length != 240 || tracks[1].Max.Kind != TrackFr {
		t.Fatalf("grid-cols class tracks = %v", div.StyleMut().GridColsList)
	}
	div = Div().Class("grid-rows-[auto_auto]")
	if tracks, ok := div.StyleMut().GridRowsList.Get(); !ok || len(tracks) != 2 || tracks[0].Min.Kind != TrackAuto || tracks[1].Max.Kind != TrackAuto {
		t.Fatalf("grid-rows class tracks = %v", div.StyleMut().GridRowsList)
	}
	div = Div().Class(`grid-areas-["head_head"_"side_main"]`)
	template, ok := div.StyleMut().GridAreas.Get()
	if !ok || strings.Join(template.AreaNames(), ",") != "head,side,main" {
		t.Fatalf("grid-areas class template = %v", div.StyleMut().GridAreas)
	}
	div = Div().Class("grid-area-[head]")
	if name, ok := div.StyleMut().GridAreaName.Get(); !ok || name != "head" {
		t.Fatalf("grid-area class name = %v", div.StyleMut().GridAreaName)
	}

	// Rejects explain themselves (a valid-but-fr-first list is NOT a
	// reject: bare fr tracks are minmax(auto, fr)).
	for _, bad := range []string{
		"grid-cols-[minmax(1fr_10px)]",
		"grid-cols-[240px_-10px]",
		"grid-cols-[]",
		"grid-areas-[head_head]",
		"grid-area-[]",
		"grid-area-[.]",
	} {
		reports := classTestCapture(t)
		Div().Class(bad)
		if len(*reports) == 0 {
			t.Errorf("Class(%q) reported nothing, want a rejection with a reason", bad)
		} else if (*reports)[0].err == nil {
			t.Errorf("Class(%q) reported a nil error", bad)
		}
	}
}
