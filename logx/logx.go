package logx

import (
	"fmt"
	"log"
	"os"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
)

var logger = log.New(os.Stdout, "", log.LstdFlags)

func genericLog(color string, level string, category string, content ...interface{}) {
	var message string
	if len(content) > 1 {
		if format, ok := content[0].(string); ok {
			message = fmt.Sprintf(format, content[1:]...)
		} else {
			message = fmt.Sprint(content...)
		}
	} else {
		message = fmt.Sprint(content...)
	}

	coloredCategory := fmt.Sprintf("%s[%s][%s]%s", color, level, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Info(category string, content ...interface{}) {
	genericLog(ColorGreen, "INFO", category, content...)
}

func Error(category string, content ...interface{}) {
	genericLog(ColorRed, "ERROR", category, content...)
}

func Warn(category string, content ...interface{}) {
	genericLog(ColorYellow, "WARN", category, content...)
}

func Debug(category string, content ...interface{}) {
	genericLog(ColorBlue, "DEBUG", category, content...)
}
