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

func InitWithOutput(output io.Writer) {
	logger.SetOutput(output)
}

func Info(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[INFO][%s]%s", ColorGreen, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Error(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[ERROR][%s]%s", ColorRed, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Fatal(category string, content ...interface{}) {
	message := fmt.Sprint(content...)
	coloredCategory := fmt.Sprintf("%s[FATAL][%s]%s", ColorRed, category, ColorReset)
	logger.Fatalf("%s: %s", coloredCategory, message)
}

func Warn(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[WARN][%s]%s", ColorYellow, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Debug(category string, content ...interface{}) {
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[DEBUG][%s]%s", ColorBlue, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

// Errorf logs an error message and returns a formatted error
func Errorf(format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	Error("ERROR", err.Error())
	return err
}
