package main

import (
	"fmt"
	"os"

	"github.com/dop251/goja"
)

func main() {
	if len(os.Args) > 99 {
		v, _ := goja.New().RunString(os.Args[1])
		fmt.Println(v)
	}
}
