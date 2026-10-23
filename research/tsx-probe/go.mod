// Throwaway probe (research/browser-parity/09): TSX via esbuild + goja.
// A nested module so the root go.mod gets no new dependencies.
module research.invalid/tsx-probe

go 1.27.0

require (
	github.com/dop251/goja v0.0.0-20261023133238-1622cea953df
	github.com/evanw/esbuild v0.28.2
	github.com/hoangvu12/gpui-go v0.0.0
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2/v2 v2.8.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/sys v0.0.0-20220715151400-c0bba94af5f8 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/hoangvu12/gpui-go => ../..
