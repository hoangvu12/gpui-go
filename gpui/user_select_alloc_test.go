package gpui

import "testing"

// userSelectAllocView renders a column of texts, selectable or opted out.
type userSelectAllocView struct {
	texts []string
	none  bool
}

func (v *userSelectAllocView) Render(w *Window, cx *Context[userSelectAllocView]) AnyElement {
	root := Div().Flex().SizeFull().FlexCol()
	for _, text := range v.texts {
		row := Div().Flex().W(Px(300))
		if v.none {
			row = row.SelectNone()
		}
		root = root.Child(row.Child(Text(text)))
	}
	return root.IntoElement()
}

// TestUserSelectIdleFrameAllocations pins the idle-frame cost of the
// automatic registration (browser-core 20's "keep the cost low while
// nothing is selected"): the same window drawn with selectable texts
// and with user-select: none texts — the none path inserts no hitbox,
// registers no participant and requests no cursor, so the per-frame
// allocation DELTA between the two is exactly what default-on selection
// costs per text: one hitbox, one registration with its run (the
// batched flush and the slot-array identity carry no per-text entity
// update), one automatic cursor request and the run's layout wrapper.
// Measured at 23 allocs per text; the gate leaves room for the frame
// machinery's own drift, which the differential cancels.
func TestUserSelectIdleFrameAllocations(t *testing.T) {
	const texts = 10
	measure := func(none bool) float64 {
		ta := NewTestApp()
		app := ta.App()
		w := NewTestWindow(app, Size{Width: 400, Height: 3000})
		t.Cleanup(func() {
			CloseTestWindow(w)
			_ = ta.App().Shutdown()
		})
		view := NewEntity(app, w.Scope(), func(v *userSelectAllocView, cx *Context[userSelectAllocView]) {
			v.none = none
			for i := 0; i < texts; i++ {
				v.texts = append(v.texts, "alpha beta gamma "+string(rune('a'+i)))
			}
		})
		w.SetRootView(ViewOf(view))
		if _, err := DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
		return float64(testing.AllocsPerRun(20, func() {
			if _, err := DrawWindowFrame(w); err != nil {
				t.Fatal(err)
			}
		}))
	}
	selectable, none := measure(false), measure(true)
	perText := (selectable - none) / texts
	t.Logf("allocs per frame: selectable %.0f, none %.0f — %.1f per text (nothing selected)", selectable, none, perText)
	if perText > 30 {
		t.Fatalf("the automatic registration costs %.1f allocs per idle text per frame, want under 30 (the hitbox, the registration with its run and the automatic cursor request)", perText)
	}
}
