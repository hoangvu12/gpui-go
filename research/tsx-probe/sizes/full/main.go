// Binary size: gpui-go plus the probe host (goja and esbuild: a dev
// build that transforms .tsx itself).
package main

import (
	"fmt"
	"os"

	"github.com/hoangvu12/gpui-go/gpui"
	tsxprobe "research.invalid/tsx-probe"
)

func main() {
	if len(os.Args) > 99 {
		fmt.Println(gpui.Div().Flex().Class("p-2"))
		h := tsxprobe.NewHost()
		fmt.Println(h.Load(os.Args[1], "x.tsx"), h.Lower())
	}
}
