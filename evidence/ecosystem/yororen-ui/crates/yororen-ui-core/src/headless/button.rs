//! Headless `button` — a clickable, focusable element with no
//! bundled visual.
//!
//! ```ignore
//! div()
//!     .bg(red).rounded(8).p_2()
//!     .apply(button("save", cx).on_click(|ev, w, cx| { ... }))
//!     .child("Save")
//! ```
//!
//! `apply` is purely a11y: it wires the focus handle, then
//! the click handler. It does **not** inject any visual
//! feedback. The caller's `el` decides colors, padding,
//! radius, hover, active — every visual concern stays
//! with the caller (or the renderer, via `default_render`).
//!
//! For the common case where the caller just wants a styled
//! button with text (and/or an icon) inside, use `caption` /
//! `icon` builder methods and then `.render(cx)` — the headless
//! wraps the content with the renderer's tokens.

use std::sync::Arc;

use gpui::{
    App, ClickEvent, Div, ElementId, FocusHandle, InteractiveElement, Pixels, SharedString,
    Stateful, StatefulInteractiveElement, Window, px,
};

// The headless `Button` marker is the same type the
// renderer registry keys on (`core::renderer::markers::Button`).
// Re-exporting it from the headless module keeps the
// `use yororen_ui_core::headless::button::Button as ButtonMarker`
// import path working in renderer code that wants the
// renderer-marker.
pub use crate::renderer::markers::Button;

use super::icon::IconSource;

/// Click handler shared by every interactive headless primitive.
pub type ClickCallback = Arc<dyn Fn(&ClickEvent, &mut Window, &mut App) + Send + Sync>;

/// Returns a `ButtonProps` with a fresh `FocusHandle` minted from `cx`.
///
/// The caller is expected to pass the result to `.apply(div)` (or the
/// renderer's `DefaultButton::default_render`).
pub fn button(id: impl Into<ElementId>, cx: &mut App) -> ButtonProps {
    ButtonProps {
        id: id.into(),
        focus_handle: cx.focus_handle(),
        on_click: None,
        disabled: false,
        clickable: true,
        variant: crate::renderer::ActionVariantKind::default(),
        caption: None,
        icon: None,
        icon_size: px(16.),
    }
}

#[derive(Clone)]
pub struct ButtonProps {
    pub id: ElementId,
    pub focus_handle: FocusHandle,
    pub on_click: Option<ClickCallback>,
    pub disabled: bool,
    pub clickable: bool,
    /// Action variant — `Neutral` (default) / `Primary` / `Danger`.
    /// The renderer dispatches to `action.<variant>.{bg,fg}`.
    pub variant: crate::renderer::ActionVariantKind,
    /// Optional text caption. Used by `render` to render the
    /// button's content. `apply` is unaffected — the caller is
    /// still expected to chain `.child(...)` for the a11y path.
    pub caption: Option<SharedString>,
    /// Optional icon source. Used by `render` to render an
    /// inline icon. When both `caption` and `icon` are set,
    /// the icon is laid out before the caption with the
    /// renderer's `icon_gap` token.
    pub icon: Option<IconSource>,
    /// Pixel size of the icon, when `icon` is set.
    pub icon_size: Pixels,
}

impl ButtonProps {
    pub fn focus_handle(&self) -> &FocusHandle {
        &self.focus_handle
    }

    pub fn is_focused(&self, window: &Window) -> bool {
        self.focus_handle.is_focused(window)
    }

    pub fn on_click<F>(mut self, listener: F) -> Self
    where
        F: 'static + Send + Sync + Fn(&ClickEvent, &mut Window, &mut App),
    {
        self.on_click = Some(Arc::new(listener));
        self
    }

    pub fn disabled(mut self, disabled: bool) -> Self {
        self.disabled = disabled;
        self
    }

    pub fn clickable(mut self, clickable: bool) -> Self {
        self.clickable = clickable;
        self
    }

    pub fn variant(mut self, v: crate::renderer::ActionVariantKind) -> Self {
        self.variant = v;
        self
    }

    /// Set the button's text caption. Consumed by `render`.
    /// `apply` is unaffected.
    pub fn caption(mut self, text: impl Into<SharedString>) -> Self {
        self.caption = Some(text.into());
        self
    }

    /// Set the button's icon source. Consumed by `render` —
    /// the icon is rendered inline with the caption (if any),
    /// using the renderer's `icon_size` / `icon_gap` tokens.
    pub fn icon(mut self, source: IconSource) -> Self {
        self.icon = Some(source);
        self
    }

    /// Override the icon's pixel size. Default: 16.
    pub fn icon_size(mut self, size: Pixels) -> Self {
        self.icon_size = size;
        self
    }

    /// Convenience: set both caption and icon in one call.
    pub fn caption_icon(mut self, caption: impl Into<SharedString>, icon: IconSource) -> Self {
        self.caption = Some(caption.into());
        self.icon = Some(icon);
        self
    }

    /// Wire the headless contract onto the caller's `el`.
    ///
    /// `apply` is purely a11y: it sets the element id,
    /// registers the focus handle, and (if `clickable` and not
    /// `disabled`) attaches the click handler. It does **not**
    /// inject any visual feedback — no opacity dip, no hover /
    /// active style. The caller (or the renderer via
    /// `default_render`) owns the visual.
    pub fn apply(self, el: Div) -> Stateful<Div> {
        let focus_handle = self.focus_handle.clone();
        let on_click = self.on_click.clone();
        let disabled = self.disabled;
        let clickable = self.clickable;
        let s = el.id(self.id.clone()).track_focus(&focus_handle);
        if clickable
            && !disabled
            && let Some(f) = on_click
        {
            s.on_click(move |ev, window, cx| {
                if disabled {
                    return;
                }
                f(ev, window, cx);
            })
        } else {
            s
        }
    }

    /// Render the button using the registered `ButtonRenderer`.
    ///
    /// Data flow is one-way: the renderer takes the full
    /// `ButtonProps` and returns a fully-built `Stateful<Div>`
    /// (visuals + children + hover / active + id + focus). This
    /// method only layers `on_click` on top of the renderer's
    /// output. **No** token values are pulled from the renderer
    /// in headless — all visual decisions live in the renderer's
    /// `compose`.
    pub fn render(self, cx: &App) -> Stateful<Div> {
        use crate::renderer::RendererContext;
        use crate::renderer::button::ButtonRenderer;
        use crate::renderer::markers::Button as ButtonMarker;

        let r: &Arc<dyn ButtonRenderer> = cx
            .renderer_arc::<ButtonMarker, dyn ButtonRenderer>()
            .expect("ButtonRenderer registered");

        let mut styled = r.compose(&self, &self.focus_handle, cx);
        // Wire a11y (click). The renderer has already set
        // the element id, visuals, children, hover / active,
        // and track_focus.
        if !self.disabled
            && self.clickable
            && let Some(f) = self.on_click.clone()
        {
            let disabled = self.disabled;
            styled = styled.on_click(move |ev, window, cx| {
                if disabled {
                    return;
                }
                f(ev, window, cx);
            });
        }
        styled
    }
}
