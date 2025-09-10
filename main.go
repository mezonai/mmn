package main

import (
	"os"
	"runtime/debug"

	"github.com/mezonai/mmn/cmd"
	"github.com/mezonai/mmn/logx"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			_ = logx.Errorf("NODE CRASHED: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()

	cmd.Execute()
}
