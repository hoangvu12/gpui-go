package base

// Port of U:input/editor/language.rs: the application's language service
// and one editor's language selection.
//
// Go adaptations:
//
//   - LanguageProvider's default methods are BaseLanguageProvider's; embed
//     it and shadow what you supply (Go's spelling of a trait default).
//   - Rc<LanguageConfig> is *LanguageConfig; the Languages global is a
//     *languageSettings stored with App.SetGlobal.
//   - The cached syntax provider is keyed by the installed service's
//     generation (bumped by SetLanguageProvider) where upstream compares
//     the Rc pointers; installing a service again replaces the cache.

import (
	"strings"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// LanguageProvider supplies language identities, editing defaults and
// syntax providers to Base (U:input/editor/language.rs:6-30).
type LanguageProvider interface {
	// LanguageName resolves a name or alias to its canonical identifier;
	// a canonical identifier resolves to itself.
	LanguageName(name string) string
	// Config is the editing defaults of a canonical identifier;
	// application registrations take precedence.
	Config(name string) *LanguageConfig
	// SyntaxContextProvider creates an editor-owned provider; nil is none.
	SyntaxContextProvider(name string) SyntaxContextProvider
}

// BaseLanguageProvider carries LanguageProvider's default bodies
// (U:input/editor/language.rs:14-29): lower-cased names, the default
// configuration and no syntax provider.
type BaseLanguageProvider struct{}

// LanguageName lower-cases the name (U:input/editor/language.rs:14-16).
func (BaseLanguageProvider) LanguageName(name string) string { return strings.ToLower(name) }

// Config is the default configuration (U:input/editor/language.rs:20-22).
func (BaseLanguageProvider) Config(string) *LanguageConfig {
	c := DefaultLanguageConfig()
	return &c
}

// SyntaxContextProvider is none (U:input/editor/language.rs:27-29).
func (BaseLanguageProvider) SyntaxContextProvider(string) SyntaxContextProvider { return nil }

// defaultLanguages serves one shared default configuration
// (U:input/editor/language.rs:32-38).
type defaultLanguages struct {
	BaseLanguageProvider
	config *LanguageConfig
}

func (d *defaultLanguages) Config(string) *LanguageConfig { return d.config }

// registeredLanguageConfig is one (name, config) registration.
type registeredLanguageConfig struct {
	name   string
	config *LanguageConfig
}

// languageSettings is LanguageSettings, the Languages global
// (U:input/editor/language.rs:40-66). Registration order preserves
// precedence when a replacement service resolves aliases.
type languageSettings struct {
	provider   LanguageProvider
	generation uint64
	configs    []registeredLanguageConfig
}

func newLanguageSettings() *languageSettings {
	c := DefaultLanguageConfig()
	return &languageSettings{provider: &defaultLanguages{config: &c}}
}

// languagesGlobal is Languages::global: the app's settings, created on
// first use (U:input/editor/language.rs:59-66).
func languagesGlobal(app *gpui.App) *languageSettings {
	if s, ok := gpui.TryGlobal[*languageSettings](app); ok {
		return s
	}
	s := newLanguageSettings()
	app.SetGlobal(s)
	return s
}

// SetLanguageProvider installs the application's language service;
// existing editors use it on their next editing operation, and registered
// configurations are resolved through it (U:input/editor/language.rs:68-73).
func SetLanguageProvider(provider LanguageProvider, app *gpui.App) {
	s := languagesGlobal(app)
	s.provider = provider
	s.generation++
}

// SetLanguageConfig replaces a language's editing configuration for this
// application; existing editors read it on their next edit. It does not
// change the auto_close or smart_indent preferences
// (U:input/editor/language.rs:75-87).
func SetLanguageConfig(language string, config LanguageConfig, app *gpui.App) {
	s := languagesGlobal(app)
	kept := s.configs[:0]
	for _, r := range s.configs {
		if r.name != language {
			kept = append(kept, r)
		}
	}
	s.configs = append(kept, registeredLanguageConfig{name: language, config: &config})
}

// languageSyntax is the cached syntax provider of one service
// (U:input/editor/language.rs:89-92).
type languageSyntax struct {
	generation uint64
	provider   SyntaxContextProvider
}

// languageSyntaxCell is Rc<RefCell<Option<LanguageSyntax>>>.
type languageSyntaxCell struct{ syntax *languageSyntax }

// editorLanguage is one editor's language selection: configurations are
// read from the shared settings, only the syntax provider is retained
// (U:input/editor/language.rs:94-101).
type editorLanguage struct {
	name      string
	languages *languageSettings
	syntax    *languageSyntaxCell
}

// newEditorLanguage is EditorLanguage::new (U:input/editor/language.rs:104-110).
func newEditorLanguage(app *gpui.App) *editorLanguage {
	return &editorLanguage{languages: languagesGlobal(app), syntax: &languageSyntaxCell{}}
}

// settings is the languages of this editor; a default EditorLanguage owns
// fresh settings (Languages::default()).
func (l *editorLanguage) settings() *languageSettings {
	if l.languages == nil {
		l.languages = newLanguageSettings()
	}
	return l.languages
}

// setName is EditorLanguage::set_name: a new name drops the cached
// syntax provider (U:input/editor/language.rs:116-119).
func (l *editorLanguage) setName(name string) {
	l.name = name
	l.syntax = &languageSyntaxCell{}
}

// config is EditorLanguage::config: the latest registration whose name
// resolves to this language, else the service's default
// (U:input/editor/language.rs:121-134).
func (l *editorLanguage) config() *LanguageConfig {
	settings := l.settings()
	source := settings.provider
	name := source.LanguageName(l.name)
	for i := len(settings.configs) - 1; i >= 0; i-- {
		if source.LanguageName(settings.configs[i].name) == name {
			return settings.configs[i].config
		}
	}
	return source.Config(name)
}

// contextAt is EditorLanguage::context_at: Code when the service has no
// syntax provider (U:input/editor/language.rs:136-154).
func (l *editorLanguage) contextAt(text rope.Rope, offset int) SyntaxContext {
	settings := l.settings()
	if l.syntax == nil {
		l.syntax = &languageSyntaxCell{}
	}
	if l.syntax.syntax == nil || l.syntax.syntax.generation != settings.generation {
		source := settings.provider
		l.syntax.syntax = &languageSyntax{
			generation: settings.generation,
			provider:   source.SyntaxContextProvider(source.LanguageName(l.name)),
		}
	}
	if p := l.syntax.syntax.provider; p != nil {
		return p.ContextAt(text, offset)
	}
	return SyntaxContextCode
}
