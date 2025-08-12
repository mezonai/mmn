package main

import (
	"fmt"
	"mmn/cmd"
	"os"
	"runtime/debug"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("NODE CRASHED: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()

	cmd.Execute()
}
