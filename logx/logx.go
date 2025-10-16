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

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var logger = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Lmicroseconds)

var currentLogLevel LogLevel = INFO

func InitWithOutput(output io.Writer) {
	logger.SetOutput(output)
	logLevelEnv := os.Getenv("LOG_LEVEL")
	if logLevelEnv != "" {
		SetLogLevelFromString(logLevelEnv)
	} else {
		currentLogLevel = DEBUG
	}
}

func SetLogLevelFromString(levelStr string) {
	switch strings.ToLower(levelStr) {
	case "debug":
		currentLogLevel = DEBUG
	case "info":
		currentLogLevel = INFO
	case "warn":
		currentLogLevel = WARN
	case "error":
		currentLogLevel = ERROR
	default:
		currentLogLevel = INFO
	}
}

func shouldLog(level LogLevel) bool {
	return level >= currentLogLevel
}

func Info(category string, content ...interface{}) {
	if !shouldLog(INFO) {
		return
	}
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[INFO][%s]%s", ColorGreen, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Error(category string, content ...interface{}) {
	if !shouldLog(ERROR) {
		return
	}
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[ERROR][%s]%s", ColorRed, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Warn(category string, content ...interface{}) {
	if !shouldLog(WARN) {
		return
	}
	message := strings.TrimSuffix(fmt.Sprintln(content...), "\n")
	coloredCategory := fmt.Sprintf("%s[WARN][%s]%s", ColorYellow, category, ColorReset)
	logger.Printf("%s: %s", coloredCategory, message)
}

func Debug(category string, content ...interface{}) {
	if !shouldLog(DEBUG) {
		return
	}
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
