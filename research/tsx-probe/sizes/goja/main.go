// Binary size: gpui-go plus goja (scripts precompiled, no esbuild: a
// release build that embeds already-transformed JS).
package main

import (
	"fmt"
	"os"

	"github.com/dop251/goja"
	"github.com/hoangvu12/gpui-go/gpui"
)

func main() {
	if len(os.Args) > 99 {
		fmt.Println(gpui.Div().Flex().Class("p-2"))
		v, _ := goja.New().RunString(os.Args[1])
		fmt.Println(v)
	}
}
