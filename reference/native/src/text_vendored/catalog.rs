// Vendored copy of `gpui_ce_parley`'s `src/catalog.rs` (the pinned checkout
// at 254b5dbd…): the FontCatalog / CatalogState / FaceRequest resolve
// machinery the vendored text system needs. The pinned file's PUBLIC
// `SystemFonts` enum is not vendored (it remains the pinned crate's own,
// imported below), and the pinned file's test-only constructors
// (`FontCatalog::new`, `CatalogState::register`, `register_fonts`) and test
// module are dropped — the vendored system only needs the shared-collection
// construction the service uses. No other change; record any further edit
// in text_vendored/mod.rs.

use anyhow::{Result, bail};
use fontique::{
    Attributes, Blob, Collection, CollectionOptions, FontStyle, FontWeight, FontWidth,
    GenericFamily, QueryFamily, QueryStatus, SourceCache,
};
use gpui_ce_parley::SystemFonts;
use parking_lot::RwLock;

#[derive(Clone)]
pub(crate) struct CatalogState {
    collection: Collection,
    sources: SourceCache,
    generation: u64,
}

impl CatalogState {
    pub(crate) fn register_blobs(&mut self, fonts: &[Blob<u8>]) -> Result<()> {
        let mut validator = Collection::new(CollectionOptions {
            shared: false,
            system_fonts: false,
        });

        for blob in fonts {
            if validator.register_fonts(blob.clone(), None).is_empty() {
                bail!("font data did not contain a supported font face");
            }
        }

        for blob in fonts {
            self.collection.register_fonts(blob.clone(), None);
        }

        self.generation = self.generation.wrapping_add(1);
        Ok(())
    }
}

/// Shared font enumeration and registration for GPUI text backends.
pub(crate) struct FontCatalog {
    pub(crate) state: RwLock<CatalogState>,
}

impl FontCatalog {
    pub(crate) fn from_shared(collection: Collection, sources: SourceCache) -> Self {
        Self {
            state: RwLock::new(CatalogState {
                collection,
                sources,
                generation: 0,
            }),
        }
    }

    /// Returns the available family names in stable display order.
    pub(crate) fn family_names(&self) -> Vec<String> {
        let mut state = self.state.write();
        let mut names = state
            .collection
            .family_names()
            .map(str::to_owned)
            .collect::<Vec<_>>();
        names.sort_unstable();
        names.dedup();
        names
    }

    /// Returns the number of successful registration batches.
    pub(crate) fn generation(&self) -> u64 {
        self.state.read().generation
    }

    /// Returns a loaded face matching the requested family and attributes.
    pub(crate) fn resolve(&self, request: &FaceRequest<'_>) -> Option<ResolvedFace> {
        resolve(&mut self.state.write(), request)
    }
}

/// Font attributes understood by the shared catalog.
#[derive(Clone, Copy, Debug)]
pub(crate) struct FaceRequest<'a> {
    /// Ordered font families to query.
    pub(crate) families: &'a [FaceFamily<'a>],
    /// OpenType weight, normally in the range 1 through 1000.
    pub(crate) weight: f32,
    /// Requested style.
    pub(crate) style: gpui::FontStyle,
    /// Optional character that the selected face must cover.
    pub(crate) character: Option<char>,
}

/// A named or generic family used for direct font resolution.
#[derive(Clone, Copy, Debug)]
pub(crate) enum FaceFamily<'a> {
    /// A concrete family name.
    Named(&'a str),
    /// The platform's user-interface font.
    SystemUi,
}

/// A font face selected by Fontique.
#[derive(Clone, Debug)]
pub(crate) struct ResolvedFace {
    /// Raw font data.
    pub(crate) data: Blob<u8>,
    /// Face index within a font collection.
    pub(crate) index: u32,
    /// Synthetic styling recommended by Fontique.
    pub(crate) synthesis: fontique::Synthesis,
}

fn resolve(state: &mut CatalogState, request: &FaceRequest<'_>) -> Option<ResolvedFace> {
    let style = match request.style {
        gpui::FontStyle::Normal => FontStyle::Normal,
        gpui::FontStyle::Italic => FontStyle::Italic,
        gpui::FontStyle::Oblique => FontStyle::Oblique(None),
    };

    let mut selected = None;
    {
        let mut query = state.collection.query(&mut state.sources);
        query.set_families(request.families.iter().map(|family| match family {
            FaceFamily::Named(name) => QueryFamily::Named(name),
            FaceFamily::SystemUi => QueryFamily::Generic(GenericFamily::SystemUi),
        }));

        query.set_attributes(Attributes::new(
            FontWidth::NORMAL,
            style,
            FontWeight::new(request.weight),
        ));
        query.matches_with(|font| {
            if request.character.is_some_and(|character| {
                font.charmap()
                    .and_then(|charmap| charmap.map(character))
                    .is_none()
            }) {
                return QueryStatus::Continue;
            }

            selected = Some((font.blob.clone(), font.index, font.synthesis));
            QueryStatus::Stop
        });
    }

    selected.map(|(data, index, synthesis)| ResolvedFace {
        data,
        index,
        synthesis,
    })
}
