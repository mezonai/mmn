package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/mezonai/mmn/cmd"
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
