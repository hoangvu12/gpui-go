package gpui

// Ticket 06 (browser-core): text-transform applied. These are the
// ported pin tests (ce:crates/gpui/src/elements/text.rs:698-761,
// text_transform_tests) plus the style-field plumbing check: the
// transform runs at the two layout entry points (div.go's text element
// and styled_text.go's TextLayout), so the element tests draw each
// transform through a real frame and read back the shaped text.

import (
	"strings"
	"testing"
)

func someTransform(t TextTransform) *TextTransform { return &t }

func TestTextTransformsPreserveBytesAndSpacing(t *testing.T) {
	input := "hello   WORLD\tfoo-bar 123baz déjà vu"
	uppercase := applyTextTransformPreservingByteLen(input, someTransform(TextTransformUppercase))
	lowercase := applyTextTransformPreservingByteLen(input, someTransform(TextTransformLowercase))
	capitalize := applyTextTransformPreservingByteLen(input, someTransform(TextTransformCapitalize))

	wantUpper := "HELLO   WORLD\tFOO-BAR 123BAZ DÉJÀ VU"
	wantLower := "hello   world\tfoo-bar 123baz déjà vu"
	wantCap := "Hello   WORLD\tFoo-Bar 123Baz Déjà Vu"
	if uppercase != wantUpper {
		t.Errorf("uppercase = %q, want %q", uppercase, wantUpper)
	}
	if lowercase != wantLower {
		t.Errorf("lowercase = %q, want %q", lowercase, wantLower)
	}
	if capitalize != wantCap {
		t.Errorf("capitalize = %q, want %q", capitalize, wantCap)
	}
	for name, got := range map[string]string{"uppercase": uppercase, "lowercase": lowercase, "capitalize": capitalize} {
		if len(got) != len(input) {
			t.Errorf("%s byte length = %d, want %d (the transform must preserve byte length so style runs keep covering the text)", name, len(got), len(input))
		}
	}
}

func TestTextTransformsSkipExpandingUnicodeMappings(t *testing.T) {
	input := "straße İSTANBUL"
	uppercase := applyTextTransformPreservingByteLen(input, someTransform(TextTransformUppercase))
	lowercase := applyTextTransformPreservingByteLen(input, someTransform(TextTransformLowercase))

	if want := "STRAßE İSTANBUL"; uppercase != want {
		t.Errorf("uppercase = %q, want %q (ß's full mapping expands, so it stays)", uppercase, want)
	}
	if want := "straße İstanbul"; lowercase != want {
		t.Errorf("lowercase = %q, want %q (İ's full mapping expands, so it stays)", lowercase, want)
	}
	if len(uppercase) != len(input) || len(lowercase) != len(input) {
		t.Errorf("skipped mappings must preserve length: %d/%d vs %d", len(uppercase), len(lowercase), len(input))
	}
}

func TestCapitalizePreservesLettersAfterDigitPrefix(t *testing.T) {
	output := applyTextTransformPreservingByteLen("123BAZ", someTransform(TextTransformCapitalize))
	if output != "123BAZ" {
		t.Errorf("capitalize = %q, want %q", output, "123BAZ")
	}
}

func TestCapitalizeDoesNotFoldRemainingLetters(t *testing.T) {
	output := applyTextTransformPreservingByteLen("foo2BAR", someTransform(TextTransformCapitalize))
	if output != "Foo2BAR" {
		t.Errorf("capitalize = %q, want %q", output, "Foo2BAR")
	}
}

func TestCapitalizeHandlesApostropheContractions(t *testing.T) {
	output := applyTextTransformPreservingByteLen("don't panic", someTransform(TextTransformCapitalize))
	if output != "Don't Panic" {
		t.Errorf("capitalize = %q, want %q", output, "Don't Panic")
	}
}

// TestNilAndNoneTransformsReturnTheText pins the no-op guards.
func TestNilAndNoneTransformsReturnTheText(t *testing.T) {
	const input = "Hello World"
	if got := applyTextTransformPreservingByteLen(input, nil); got != input {
		t.Errorf("nil transform = %q, want the input", got)
	}
	if got := applyTextTransformPreservingByteLen(input, someTransform(TextTransformNone)); got != input {
		t.Errorf("None transform = %q, want the input", got)
	}
}

// transformCaptureSystem records the text of every document painted
// through the ordinary text paint (the pattern of text_truncate_test
// .go's truncatePaintSystem): the element test reads the text the text
// element actually shaped, which is where the transform must show up.
type transformCaptureSystem struct {
	*testTextSystem
	painted *[]string
}

func (r *transformCaptureSystem) shape(text string, fontSize float32, font FontDescriptor, wrapWidth *float32, lineClamp *uint32) (shapedText, error) {
	*r.painted = append(*r.painted, text)
	return r.testTextSystem.shape(text, fontSize, font, wrapWidth, lineClamp)
}

// drawTransformedText draws one Div carrying the given text-transform
// with a text child, and returns the text the frame shaped.
func drawTransformedText(t *testing.T, transform *TextTransform, text string) string {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	var painted []string
	drawState(window).text = &transformCaptureSystem{testTextSystem: &testTextSystem{}, painted: &painted}
	root := transformRoot{build: func() AnyElement {
		return Div().Flex().TextTransform(*transform).TextSize(Px(16)).Child(text).IntoElement()
	}}
	window.SetRootView(root)
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	if len(painted) == 0 {
		t.Fatal("the frame shaped no text")
	}
	return painted[len(painted)-1]
}

// transformRoot is the stateless root view of the transform element
// tests (the recipeView pattern of helpers_draw_windows_test.go): it
// rebuilds its tree every frame like any Render.
type transformRoot struct {
	build func() AnyElement
}

func (r transformRoot) EntityID() (EntityID, bool)          { return 0, false }
func (r transformRoot) RenderOnce(*Window, *App) AnyElement { return r.build() }

// TestTextElementAppliesTextTransform pins the application point: the
// text element (div.go textElement.RequestLayout, the pin's
// TextLayout::layout at text.rs:895) shapes the TRANSFORMED text, while
// the element keeps its source text (a11y still reports the source,
// like the pin's Text::write_a11y_info).
func TestTextElementAppliesTextTransform(t *testing.T) {
	const source = "hello WORLD déjà"
	cases := []struct {
		name      string
		transform TextTransform
		want      string
	}{
		{"uppercase", TextTransformUppercase, "HELLO WORLD DÉJÀ"},
		{"lowercase", TextTransformLowercase, "hello world déjà"},
		{"capitalize", TextTransformCapitalize, "Hello WORLD Déjà"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := drawTransformedText(t, someTransform(c.transform), source); got != c.want {
				t.Errorf("shaped text = %q, want %q", got, c.want)
			}
		})
	}
}

// TestStyledTextAppliesTextTransform pins the second entry point:
// StyledText's TextLayout (styled_text.go layout, the pin's
// TextLayout::layout) shapes the transformed text too.
func TestStyledTextAppliesTextTransform(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	var painted []string
	drawState(window).text = &transformCaptureSystem{testTextSystem: &testTextSystem{}, painted: &painted}
	window.SetRootView(transformRoot{build: func() AnyElement {
		return Div().Flex().TextTransform(TextTransformUppercase).TextSize(Px(16)).Child(
			NewStyledText("hello styled").IntoElement()).IntoElement()
	}})
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	if len(painted) == 0 {
		t.Fatal("the frame shaped no text")
	}
	last := painted[len(painted)-1]
	if !strings.Contains(last, "HELLO STYLED") {
		t.Errorf("StyledText shaped %q, want the uppercase text", last)
	}
}
