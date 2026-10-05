//----------------------------------------
//
// Copyright © yanghy. All Rights Reserved.
//
// Licensed under Apache License 2.0
//
//----------------------------------------

package main

import (
	"github.com/energye/gpui/examples"
	"github.com/energye/gpui/render"
	"github.com/energye/gpui/ui"
	"github.com/energye/lcl/api/libname"
)

func main() {
	libname.UseWS = "gtk3"

	app := ui.NewApplication()

	face := examples.Face

	// Main window: blue circle
	mainWin := ui.NewWindow(ui.WindowConfig{
		Title:  "GPUI - Main Window",
		Width:  800,
		Height: 600,
	})
	mainWin.OnInit(func(ctrl *ui.TGPUControl) {
		ctrl.SetOnRender(func(ctx *render.Context) {
			ctx.ClearWithColor(render.RGBA{R: 0.95, G: 0.95, B: 0.98, A: 1})
			ctx.SetRGBA(0.2, 0.4, 0.8, 1.0)
			ctx.DrawCircle(400, 300, 200)
			ctx.Fill()
			ctx.SetFont(face(14))
			ctx.SetRGBA(0, 0, 0, 1)
			ctx.DrawString("Main Window", 350, 320)
		})
	})

	// Second window: red rectangle
	secondWin := ui.NewWindow(ui.WindowConfig{
		Title:  "GPUI - Second Window",
		Width:  400,
		Height: 300,
	})
	secondWin.OnInit(func(ctrl *ui.TGPUControl) {
		ctrl.SetOnRender(func(ctx *render.Context) {
			ctx.ClearWithColor(render.RGBA{R: 1, G: 1, B: 0.95, A: 1})
			ctx.SetRGBA(0.8, 0.2, 0.2, 1.0)
			ctx.DrawRectangle(50, 50, 300, 200)
			ctx.Fill()
			ctx.SetFont(face(14))
			ctx.SetRGBA(0, 0, 0, 1)
			ctx.DrawString("Second Window", 120, 160)
		})
	})

	// Third window: green triangle
	thirdWin := ui.NewWindow(ui.WindowConfig{
		Title:  "GPUI - Third Window",
		Width:  400,
		Height: 300,
	})
	thirdWin.OnInit(func(ctrl *ui.TGPUControl) {
		ctrl.SetOnRender(func(ctx *render.Context) {
			ctx.ClearWithColor(render.RGBA{R: 0.95, G: 1, B: 0.95, A: 1})
			ctx.SetRGBA(0.2, 0.8, 0.2, 1.0)
			ctx.MoveTo(200, 40)
			ctx.LineTo(350, 260)
			ctx.LineTo(50, 260)
			ctx.ClosePath()
			ctx.Fill()
			ctx.SetFont(face(14))
			ctx.SetRGBA(0, 0, 0, 1)
			ctx.DrawString("Third Window", 150, 160)
		})
	})

	app.AddWindow(mainWin)
	app.AddWindow(secondWin)
	app.AddWindow(thirdWin)
	app.Run()
}
