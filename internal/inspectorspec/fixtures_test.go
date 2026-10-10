package inspectorspec

import (
	"testing"

	"gpui-go/gpui"
)

// inspectorFixture is one test window with its virtualized corpus.
type inspectorFixture struct {
	testApp *gpui.TestApp
	app     *gpui.App
	window  *gpui.Window
	model   gpui.Entity[PanelModel]
}

// newInspectorFixture builds the app, window, model and root view: a
// 800x300 window whose root renders the header plus a 20px-pitch
// uniform list (the virtualized custom-component tree).
func newInspectorFixture(t *testing.T, itemCount int) *inspectorFixture {
	t.Helper()
	testApp := gpui.NewTestApp()
	app := testApp.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 800, Height: 300})
	model := newPanelModel(app, itemCount)
	window.SetRootView(&panelView{model: model})
	return &inspectorFixture{testApp: testApp, app: app, window: window, model: model}
}

// draw draws one frame and fails on error.
func (f *inspectorFixture) draw(t *testing.T) {
	t.Helper()
	if _, err := gpui.DrawWindowFrame(f.window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
}

// toggleInspector toggles the window's inspector on.
func (f *inspectorFixture) toggleInspector(t *testing.T) {
	t.Helper()
	f.window.ToggleInspector(f.app)
}

// mutate applies one model mutation.
func (f *inspectorFixture) mutate(t *testing.T, mutate func(m *PanelModel, cx *gpui.Context[PanelModel])) {
	t.Helper()
	f.model.Update(f.app, mutate)
}

// itemNodeAt returns the tree node of the item whose bounds contain
// the given point (the picking-side resolution helper).
func itemNodeAt(t *testing.T, snapshot *gpui.InspectorTreeSnapshot, y float32) gpui.InspectorTreeNode {
	t.Helper()
	for _, node := range snapshot.Nodes() {
		if node.Location == itemSource && node.Bounds.Origin.Y <= y && y < node.Bounds.Origin.Y+node.Bounds.Size.Height {
			return node
		}
	}
	t.Fatalf("no item node at y=%v", y)
	return gpui.InspectorTreeNode{}
}

// headerNode returns the header's tree node.
func headerNode(t *testing.T, snapshot *gpui.InspectorTreeSnapshot) gpui.InspectorTreeNode {
	t.Helper()
	for _, node := range snapshot.Nodes() {
		if node.Location == headerSource {
			return node
		}
	}
	t.Fatalf("no header node")
	return gpui.InspectorTreeNode{}
}
