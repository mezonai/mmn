package exception

import (
	"os"
	"runtime/debug"

	"github.com/mezonai/mmn/logx"
)

func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logx.Error("Panic in: ", name, r, string(debug.Stack()))
			}
		}()
		fn()
	}()
}

func SafeGoWithPanic(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logx.Error("Panic in: ", name, r, string(debug.Stack()))
				os.Exit(1)
			}
		}()
		fn()
	}()
}
