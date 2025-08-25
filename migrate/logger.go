package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

// LogLevel represents different log levels
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// String returns the string representation of log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging functionality
type Logger struct {
	level    LogLevel
	logger   *log.Logger
	enabled  bool
}

// Global logger instance
var globalLogger *Logger

// InitLogger initializes the global logger
func InitLogger(level LogLevel) {
	globalLogger = &Logger{
		level:   level,
		logger:  log.New(os.Stdout, "", 0),
		enabled: true,
	}
}

// SetLogLevel sets the minimum log level
func SetLogLevel(level LogLevel) {
	if globalLogger != nil {
		globalLogger.level = level
	}
}

// logf formats and logs a message with the given level
func (l *Logger) logf(level LogLevel, format string, args ...interface{}) {
	if !l.enabled || level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	levelStr := level.String()
	message := fmt.Sprintf(format, args...)

	// Color coding for different log levels
	var colorCode string
	switch level {
	case DEBUG:
		colorCode = "\033[36m" // Cyan
	case INFO:
		colorCode = "\033[32m" // Green
	case WARN:
		colorCode = "\033[33m" // Yellow
	case ERROR:
		colorCode = "\033[31m" // Red
	case FATAL:
		colorCode = "\033[35m" // Magenta
	default:
		colorCode = "\033[0m" // Reset
	}
	resetCode := "\033[0m"

	formattedMessage := fmt.Sprintf("%s[%s] %s%s %s",
		colorCode, timestamp, levelStr, resetCode, message)

	l.logger.Println(formattedMessage)

	// Exit on fatal errors
	if level == FATAL {
		os.Exit(1)
	}
}

// Global logging functions
func LogDebug(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.logf(DEBUG, format, args...)
	}
}

func LogInfo(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.logf(INFO, format, args...)
	}
}

func LogWarn(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.logf(WARN, format, args...)
	}
}

func LogError(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.logf(ERROR, format, args...)
	}
}

func LogFatal(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.logf(FATAL, format, args...)
	}
}

// Structured logging with context
func LogWithContext(level LogLevel, context string, format string, args ...interface{}) {
	if globalLogger != nil {
		contextualFormat := fmt.Sprintf("[%s] %s", context, format)
		globalLogger.logf(level, contextualFormat, args...)
	}
}

// Migration-specific logging functions
func LogMigrationStart(userCount int) {
	LogInfo("🚀 Starting migration process for %d users", userCount)
}

func LogMigrationComplete(processed, successful int) {
	LogInfo("✅ Migration completed: %d/%d users processed successfully", successful, processed)
}

func LogUserProcessing(userID int, userName string) {
	LogDebug("👤 Processing user %d (%s)", userID, userName)
}

func LogWalletCreated(userID int, address string) {
	LogInfo("💰 Wallet created for user %d - Address: %s", userID, address)
}

func LogTokenTransfer(fromAddr, toAddr string, amount uint64) {
	LogInfo("💸 Token transfer: %s → %s (amount: %d)", fromAddr, toAddr, amount)
}

func LogDatabaseOperation(operation, table string, affected int) {
	LogDebug("🗄️ Database %s on %s: %d rows affected", operation, table, affected)
}

func LogConnectionTest(service, endpoint string, success bool) {
	if success {
		LogInfo("🔗 %s connection successful (endpoint: %s)", service, endpoint)
	} else {
		LogError("❌ %s connection failed (endpoint: %s)", service, endpoint)
	}
}