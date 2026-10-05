module github.com/energye/gpui

go 1.25.0

require (
	github.com/ebitengine/purego v0.10.1
	github.com/energye/lcl v1.0.9
	github.com/gogpu/gpucontext v0.21.0
	github.com/gogpu/gputypes v0.5.1
	github.com/gogpu/naga v0.17.15
	github.com/gogpu/wgpu v0.30.10
	golang.org/x/image v0.44.0
	golang.org/x/text v0.40.0
)

require (
	github.com/go-webgpu/goffi v0.5.6 // indirect
	github.com/go-webgpu/webgpu v0.5.2 // indirect
)

replace github.com/energye/lcl => ../lcl
