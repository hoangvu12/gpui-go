// Binary-size baseline: gpui-go only (the plain-Go sidebar).
package main

import (
	"fmt"
	"os"

	"github.com/hoangvu12/gpui-go/gpui"
)

func main() {
	if len(os.Args) > 99 {
		fmt.Println(gpui.Div().Flex().Class("p-2"))
	}
}
