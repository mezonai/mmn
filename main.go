package main

import (
	"log"
	"mmn/cmd"
	"runtime/debug"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[FATAL] Panic: %v\n%s", r, debug.Stack())
		}
	}()

	cmd.Execute()
}
