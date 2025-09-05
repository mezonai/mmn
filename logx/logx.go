package logx

import (
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"log"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
)

var (
	lumberjackLogger = &lumberjack.Logger{
		Filename: "./mmn.log",
		MaxSize:  500, // megabytes
		MaxAge:   7,   // days
		Compress: true,
	}

	logger = log.New(lumberjackLogger, "", log.Ldate|log.Ltime|log.Lmicroseconds)
)

func Info(category string, content ...interface{}) {
	message := fmt.Sprint(content...)
	coloredCategory := fmt.Sprintf("%s[%s]%s", ColorGreen, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Error(category string, content ...interface{}) {
	message := fmt.Sprint(content...)
	coloredCategory := fmt.Sprintf("%s[ERROR][%s]%s", ColorRed, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Warn(category string, content ...interface{}) {
	message := fmt.Sprint(content...)
	coloredCategory := fmt.Sprintf("%s[WARN][%s]%s", ColorYellow, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Debug(category string, content ...interface{}) {
	message := fmt.Sprint(content...)
	coloredCategory := fmt.Sprintf("%s[DEBUG][%s]%s", ColorBlue, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

// Errorf logs an error message and returns a formatted error
func Errorf(format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	Error("ERROR", err.Error())
	return err
}
