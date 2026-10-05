package authoring_test

import (
	"fmt"
	"strings"
	"testing"

	"gpui-go/authoring"
)

// These types deliberately live outside package authoring. Promoted methods
// must preserve their concrete types without access to private helper state.
type Button struct {
	authoring.Styled[*Button]
	disabled bool
}

func NewButton() *Button                      { return authoring.BindStyled(&Button{}) }
func (b *Button) Disabled(value bool) *Button { b.disabled = value; return b }

type Label struct {
	authoring.Styled[*Label]
	text string
}

func NewLabel() *Label                    { return authoring.BindStyled(&Label{}) }
func (l *Label) Text(value string) *Label { l.text = value; return l }

func TestChainsPreserveDifferentExternalComponentTypes(t *testing.T) {
	b := NewButton()
	var result *Button = b.Flex().Disabled(true).Px3().Disabled(false).Gap2()
	if result != b || b.disabled {
		t.Fatal("chain lost the concrete component or its custom state")
	}
	l := NewLabel()
	var label *Label = l.Opacity(0).Text("Hello").Px3().Text("GPUI")
	if label != l || l.text != "GPUI" {
		t.Fatal("second external type lost its own methods")
	}
	if _, present := l.Style().Display.Get(); present {
		t.Fatal("different instances shared style")
	}
	left, present := b.Style().PaddingLeft.Get()
	if !present || left != authoring.Rem(0.75) {
		t.Fatalf("padding = %v, present = %v", left, present)
	}
	gap, present := b.Style().Gap.Get()
	if !present || gap != authoring.Rem(0.5) {
		t.Fatalf("gap = %v, present = %v", gap, present)
	}
}

func TestRefinementPresenceAndSnapshots(t *testing.T) {
	b := NewButton().Flex().PaddingX(authoring.Px(12)).Opacity(1)
	snapshot := b.Style()
	b.Refine(authoring.StyleRefinement{Opacity: authoring.Some(float32(0))})
	current := b.Style()
	if value, present := current.Opacity.Get(); !present || value != 0 {
		t.Fatal("explicit zero override was lost")
	}
	if value, _ := snapshot.Opacity.Get(); value != 1 {
		t.Fatal("snapshot changed after mutation")
	}
	snapshot.Display = authoring.Some(authoring.DisplayBlock)
	if value, _ := b.Style().Display.Get(); value != authoring.DisplayFlex {
		t.Fatal("snapshot mutation escaped into component")
	}
	if value, _ := b.Style().PaddingRight.Get(); value != authoring.Px(12) {
		t.Fatal("absent refinement erased padding or changed units")
	}
	b.Refine(authoring.StyleRefinement{})
	if b.Style() != current {
		t.Fatal("empty refinement changed existing style")
	}
	if value, present := authoring.Some(false).Get(); !present || value {
		t.Fatal("explicit false is not representable")
	}
}

func TestCopiesFailBeforeMutatingAndAliasesRemainValid(t *testing.T) {
	b := NewButton().Opacity(1)
	copy := *b
	wantPanic(t, "copied bound component", func() { copy.Opacity(0) })
	wantPanic(t, "copied bound component", func() { copy.Style() })
	wantPanic(t, "already bound", func() { authoring.BindStyled(&copy) })
	if opacity, _ := b.Style().Opacity.Get(); opacity != 1 {
		t.Fatal("rejected copy changed original")
	}
	alias := b
	if alias.Opacity(0) != b {
		t.Fatal("alias did not return shared owner")
	}
	if opacity, _ := b.Style().Opacity.Get(); opacity != 0 {
		t.Fatal("alias should share mutation")
	}
}

func TestBindingMisuseFailsClearly(t *testing.T) {
	var nilButton *Button
	wantPanic(t, "nil component", func() { authoring.BindStyled(nilButton) })
	var zero Button
	wantPanic(t, "unbound Styled", func() { zero.Flex() })
	wantPanic(t, "unbound Styled", func() { zero.Style() })
	b := NewButton()
	wantPanic(t, "already bound", func() { authoring.BindStyled(b) })
	var nilHelper *authoring.Styled[*Button]
	wantPanic(t, "unbound Styled", func() { nilHelper.Flex() })
	// An unbound zero-value component can be copied and then bound independently.
	second := zero
	authoring.BindStyled(&zero).Opacity(1)
	authoring.BindStyled(&second).Opacity(0)
	if value, _ := zero.Style().Opacity.Get(); value != 1 {
		t.Fatal("binding independent values shared state")
	}
}

func wantPanic(t *testing.T, message string, f func()) {
	t.Helper()
	defer func() {
		value := recover()
		if value == nil || !strings.Contains(fmt.Sprint(value), message) {
			t.Errorf("panic = %v; want message containing %q", value, message)
		}
	}()
	f()
}

func ExampleBindStyled() {
	button := NewButton().Flex().Px3().Disabled(true).Gap2()
	padding, _ := button.Style().PaddingLeft.Get()
	fmt.Println(button.disabled, padding.Value, padding.Unit == authoring.Rems)
	// Output: true 0.75 true
}
