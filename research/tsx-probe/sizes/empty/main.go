package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 99 {
		fmt.Println(os.Args)
	}
}
