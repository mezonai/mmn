package logx

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
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
		Filename: getLogFilename(),
		MaxSize:  500, // megabytes
		MaxAge:   7,   // days
	}

	logger = log.New(lumberjackLogger, "", log.Ldate|log.Ltime|log.Lmicroseconds)
)

func getLogFilename() string {
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		return "./logs/" + logFile
	}
	return "./logs/mmn.log"
}

func Info(category string, content ...interface{}) {
	message := fmt.Sprint(content...)
	coloredCategory := fmt.Sprintf("%s[INFO][%s]%s", ColorGreen, category, ColorReset)
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
