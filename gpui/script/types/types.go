// Package types is the TSX surface's type-checking half (ticket 63,
// ADR 0003): it carries the generated .d.ts (gpui-script.d.ts, written
// by cmd/tsxdtsgen from the engine's own source and kept in sync by
// the drift tests in internal/tsxgen) and runs tsc against a
// directory of .tsx sources with it — the engine behind
// script.WithTypeCheck's reload checks and cmd/tsxcheck.
//
// The host package never imports this one: script.WithTypeCheck takes
// the TypeChecker interface, so an app opts in and a release build
// that does not check types links none of this (no tsc discovery, no
// embedded declarations).
//
//go:generate go run github.com/hoangvu12/gpui-go/cmd/tsxdtsgen
package types

import (
	_ "embed"
)

// dts is the generated ambient declaration file: the elements, style
// properties, events, globals and Tailwind classes the engine accepts,
// plus the plugin surface (ticket 67: the slot element's SlotProps
// and the PluginManifest a plugin bundle exports).
//
//go:embed gpui-script.d.ts
var dts string

// DTS returns the generated gpui-script.d.ts (the engine's TSX surface
// as ambient declarations). Write it beside your sources with
// cmd/tsxcheck -init so editors see the same types the check does.
func DTS() string { return dts }

// DTSFilename is the file name the declarations are written as.
const DTSFilename = "gpui-script.d.ts"
