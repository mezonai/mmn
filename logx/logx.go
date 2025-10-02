package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
)

var logger = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Lmicroseconds)

var trackLogMetrics func(level string, sizeBytes int)

func InitWithOutput(output io.Writer) {
	logger.SetOutput(output)
}

func SetMetricsTracker(tracker func(level string, sizeBytes int)) {
	trackLogMetrics = tracker
}

func trackLog(level string, message string) {
	if trackLogMetrics != nil {
		trackLogMetrics(level, len(message))
	}
}

func Info(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[INFO][%s]%s", ColorGreen, category, ColorReset)
	fullMessage := fmt.Sprintf("%s: %s", coloredCategory, message)
	logger.Print(fullMessage)
	trackLog("info", fullMessage)
}

func Error(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[ERROR][%s]%s", ColorRed, category, ColorReset)
	fullMessage := fmt.Sprintf("%s: %s", coloredCategory, message)
	logger.Print(fullMessage)
	trackLog("error", fullMessage)
}

func Warn(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[WARN][%s]%s", ColorYellow, category, ColorReset)
	fullMessage := fmt.Sprintf("%s: %s", coloredCategory, message)
	logger.Print(fullMessage)
	trackLog("warn", fullMessage)
}

func Debug(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[DEBUG][%s]%s", ColorBlue, category, ColorReset)
	fullMessage := fmt.Sprintf("%s: %s", coloredCategory, message)
	logger.Print(fullMessage)
	trackLog("debug", fullMessage)
}

// Errorf logs an error message and returns a formatted error
func Errorf(format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	Error("ERROR", err.Error())
	return err
}
