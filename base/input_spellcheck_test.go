package base

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Spellcheck tests (browser-core ticket 30): the text input's opt-in
// hook — requests queue from the element's prepaint when the document
// changed, delivered misspellings draw as one red wavy underline per
// misspelled word (Blink's misspelling marker), stale results are
// superseded, and the default is off.
//
// buildSpellcheckInput is buildInput (input_state_test.go) with the
// application's spellcheck service injected BEFORE the first frame:
// the first draw already runs the element's prepaint, so a spellcheck
// opt-in would otherwise spin up the platform-default (real COM)
// service before the fake lands.

// inputFakeChecker is the base-side fake: misspellings by exact text,
// plus a switchable failure mode (an error for every later check).
type inputFakeChecker struct {
	misspellBy map[string][]gpui.Misspelling
	fail       atomic.Bool
	checks     []string
}

func (f *inputFakeChecker) Prepare() error { return nil }

func (f *inputFakeChecker) Check(text, lang string) ([]gpui.Misspelling, error) {
	f.checks = append(f.checks, text)
	if f.fail.Load() {
		return nil, errors.New("the dictionary went away")
	}
	return f.misspellBy[text], nil
}

func (f *inputFakeChecker) Suggest(word, lang string) ([]string, error) { return nil, nil }
func (f *inputFakeChecker) Add(word, lang string) error                 { return nil }
func (f *inputFakeChecker) Ignore(word, lang string) error              { return nil }
func (f *inputFakeChecker) Release()                                    {}

// buildSpellcheckInput opens an input view whose app carries a fake
// spellcheck service, and returns both.
func buildSpellcheckInput(t *testing.T, fake gpui.SpellChecker, f func(InputState) InputState) (*inputView[InputMode], *gpui.SpellcheckService) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	service := gpui.NewSpellcheckService(ta.App(), fake)
	ta.App().SetSpellcheck(service)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	var input gpui.Entity[InputState]
	_, cx := ta.AddWindowView(func(r *inputTestRoot[InputMode], w *gpui.Window, cx *gpui.Context[inputTestRoot[InputMode]]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *InputState, scx *gpui.Context[InputState]) {
			*s = f(NewInputState(w, scx))
		})
		r.input = input
	})
	return &inputView[InputMode]{input: input, cx: cx}, service
}

// spellcheckRanges polls the delivery queue and the dispatcher until
// the input's misspelled ranges equal want (the delivered check
// repaints through Notify).
func spellcheckRangesEqual(t *testing.T, v *inputView[InputMode], svc *gpui.SpellcheckService, want []gpui.TextRange) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		svc.FlushDeliveries()
		v.cx.RunUntilParked()
		var got []gpui.TextRange
		v.read(func(s *InputBaseState[InputMode]) { got = s.SpellcheckRanges() })
		if len(got) == len(want) {
			same := true
			for i := range want {
				if got[i] != want[i] {
					same = false
				}
			}
			if same {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("spellcheck ranges = %v, want %v", got, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// inputUnderlines draws one frame and returns its underline records.
func inputUnderlines(t *testing.T, v *inputView[InputMode]) []gpui.Underline {
	t.Helper()
	scene, err := gpui.DrawWindowFrame(v.cx.Window())
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	records, err := scene.Underlines()
	if err != nil {
		t.Fatalf("scene underlines: %v", err)
	}
	return records
}

// TestInputSpellcheckIsOffByDefault: without the opt-in no check is
// requested and nothing draws an underline (the explicit opt-in
// decision; HTML spellcheck's UA default is inverted here on purpose).
func TestInputSpellcheckIsOffByDefault(t *testing.T) {
	fake := &inputFakeChecker{misspellBy: map[string][]gpui.Misspelling{
		"quikc": {{Start: 0, End: 5}},
	}}
	v, svc := buildSpellcheckInput(t, fake, func(s InputState) InputState {
		return s.DefaultValue("quikc")
	})
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputState]) {
		s.Insert("x", w, cx)
	})
	v.cx.RunUntilParked()
	svc.FlushDeliveries()
	v.cx.RunUntilParked()
	if len(fake.checks) != 0 {
		t.Fatalf("checks without the opt-in = %v, want none", fake.checks)
	}
	if records := inputUnderlines(t, v); len(records) != 0 {
		t.Fatalf("underline records = %d, want 0", len(records))
	}
}

// TestInputSpellcheckDrawsWavyRedUnderlineForMisspelledWord: an opted-in
// input with a misspelled word draws exactly one underline record —
// wavy, red, spanning the misspelled word (Blink's misspelling marker:
// layout_theme.cc:413-416's red over text_decoration_info.cc:467-481's
// wavy stroke).
func TestInputSpellcheckDrawsWavyRedUnderlineForMisspelledWord(t *testing.T) {
	fake := &inputFakeChecker{misspellBy: map[string][]gpui.Misspelling{
		"hello quikc world": {{Start: 6, End: 11}},
	}}
	v, svc := buildSpellcheckInput(t, fake, func(s InputState) InputState {
		return s.Spellcheck(true).DefaultValue("hello quikc world")
	})
	spellcheckRangesEqual(t, v, svc, []gpui.TextRange{{Start: 6, End: 11}})

	records := inputUnderlines(t, v)
	if len(records) != 1 {
		t.Fatalf("underline records = %d, want 1 (the misspelled word)", len(records))
	}
	record := records[0]
	if !record.Wavy {
		t.Errorf("the misspelling underline is not wavy")
	}
	if record.Color != gpui.Red() {
		t.Errorf("underline color = %v, want the spelling marker red %v", record.Color, gpui.Red())
	}

	// The record spans the misspelled word only: its width tracks the
	// shaped width of "quikc" (through the window's text system — the
	// deterministic one a test window draws with), far from the whole
	// line's.
	w := v.cx.Window()
	defaultFont := gpui.DefaultTextStyle().FontDescriptorOf()
	shape := func(text string) float32 {
		line, err := w.TextSystem().ShapeLine(text, gpui.DefaultRemSize, []gpui.TextRun{{Len: len(text), Font: defaultFont, Color: gpui.Black()}}, nil)
		if err != nil {
			t.Fatalf("shaping %q: %v", text, err)
		}
		return line.Width()
	}
	wordWidth := shape("quikc") * gpui.TestScaleFactor
	fullWidth := shape("hello quikc world") * gpui.TestScaleFactor
	if got := record.Bounds.Size.Width; got < wordWidth*0.75 || got > wordWidth*1.25 {
		t.Errorf("underline width = %v, want within 25%% of the shaped \"quikc\" width %v", got, wordWidth)
	}
	if got := record.Bounds.Size.Width; got > fullWidth*0.6 {
		t.Errorf("underline width = %v spans most of the line (width %v); it must cover only the misspelled word", got, fullWidth)
	}
}

// TestInputSpellcheckSupersedesStaleResultWhileTyping: while the user
// types, the misspelled ranges of the LATEST text are drawn — the
// service supersedes the older request and the delivery guard drops a
// result whose text no longer matches.
func TestInputSpellcheckSupersedesStaleResultWhileTyping(t *testing.T) {
	fake := &inputFakeChecker{misspellBy: map[string][]gpui.Misspelling{
		"quikc":  {{Start: 0, End: 5}},
		"xquikc": {{Start: 1, End: 6}},
	}}
	v, svc := buildSpellcheckInput(t, fake, func(s InputState) InputState {
		return s.Spellcheck(true).DefaultValue("quikc")
	})
	// The initial value's check is queued by the first frame; type one
	// more character at the caret (DefaultValue leaves it at the start,
	// so the typed text is "xquikc") before flushing anything: the newer
	// request supersedes the queued one.
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputState]) {
		s.Insert("x", w, cx)
	})
	v.cx.RunUntilParked()
	spellcheckRangesEqual(t, v, svc, []gpui.TextRange{{Start: 1, End: 6}})

	records := inputUnderlines(t, v)
	if len(records) != 1 || !records[0].Wavy {
		t.Fatalf("underline records = %v, want one wavy record for the latest text", records)
	}
	// Only the latest text was checked-and-kept: the older result for
	// "quikc" must not survive as a stale range (whichever layer
	// dropped it — the service's supersession or the text guard).
	v.read(func(s *InputBaseState[InputMode]) {
		if got := s.SpellcheckRanges(); len(got) != 1 || got[0] != (gpui.TextRange{Start: 1, End: 6}) {
			t.Fatalf("spellcheck ranges = %v, want [1..6] for \"xquikc\"", got)
		}
	})
}

// TestInputSpellcheckSkipsMaskedInputs: a masked input never sends its
// text to the checker (password fields must not leak their contents).
func TestInputSpellcheckSkipsMaskedInputs(t *testing.T) {
	fake := &inputFakeChecker{misspellBy: map[string][]gpui.Misspelling{
		"secret": {{Start: 0, End: 6}},
	}}
	v, svc := buildSpellcheckInput(t, fake, func(s InputState) InputState {
		return s.Spellcheck(true).Masked(true).DefaultValue("secret")
	})
	v.cx.RunUntilParked()
	svc.FlushDeliveries()
	v.cx.RunUntilParked()
	if len(fake.checks) != 0 {
		t.Fatalf("masked input checks = %v, want none", fake.checks)
	}
	if records := inputUnderlines(t, v); len(records) != 0 {
		t.Fatalf("underline records = %d, want 0", len(records))
	}
}

// TestInputSpellcheckClearsMarkersWhenTheCheckFails: a failed check
// (the dictionary went away) clears the previous markers instead of
// drawing stale ones.
func TestInputSpellcheckClearsMarkersWhenTheCheckFails(t *testing.T) {
	failing := &inputFakeChecker{misspellBy: map[string][]gpui.Misspelling{
		"quikc": {{Start: 0, End: 5}},
	}}
	v, svc := buildSpellcheckInput(t, failing, func(s InputState) InputState {
		return s.Spellcheck(true).DefaultValue("quikc")
	})
	spellcheckRangesEqual(t, v, svc, []gpui.TextRange{{Start: 0, End: 5}})
	// A re-check of the same text now fails: the stale markers clear.
	failing.fail.Store(true)
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputState]) {
		s.SetValue("quikc", w, cx)
	})
	v.cx.RunUntilParked()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc.FlushDeliveries()
		v.cx.RunUntilParked()
		if len(inputUnderlines(t, v)) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the stale markers did not clear after a failed check")
}

// TestInputSpellcheckHighlightStyleIsWavyRed pins the marker style
// itself (the diagnostics' wavy-underline shape with the spelling red).
func TestInputSpellcheckHighlightStyleIsWavyRed(t *testing.T) {
	style := spellcheckHighlightStyle()
	if style.Underline == nil || !style.Underline.Wavy {
		t.Fatalf("marker style = %+v, want a wavy underline", style)
	}
	if style.Underline.Color == nil || *style.Underline.Color != gpui.Red() {
		t.Fatalf("marker color = %v, want %v", style.Underline.Color, gpui.Red())
	}
}

// TestInputSpellcheckByteRangeConversion pins the UTF-16 -> byte range
// conversion, including a non-ASCII prefix (the checker reports UTF-16
// code units; the highlight styles use document bytes).
func TestInputSpellcheckByteRangeConversion(t *testing.T) {
	text := "héllo wörld" // é and ö are 2 bytes each, 1 UTF-16 unit
	got := spellcheckByteRanges(text, []gpui.Misspelling{
		{Start: 0, End: 5},  // "héllo" -> bytes 0..6
		{Start: 6, End: 11}, // "wörld" -> bytes 7..13
		{Start: 3, End: 3},  // empty: dropped
	})
	want := []gpui.TextRange{{Start: 0, End: 6}, {Start: 7, End: 13}}
	if len(got) != len(want) {
		t.Fatalf("ranges = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranges[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
