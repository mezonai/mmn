package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mezonai/mmn/logx"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// LogLevel represents different log levels
type LogLevel int

const (
	INFO LogLevel = iota
	ERROR
	RESULT
)

// SystemMetrics holds system resource metrics
type SystemMetrics struct {
	CPUPercent    float64
	MemoryPercent float64
	DiskRead      uint64
	DiskWrite     uint64
	NetworkRx     uint64
	NetworkTx     uint64
	Timestamp     time.Time
}

// Logger handles file logging for stress tests
type Logger struct {
	infoFile       *os.File
	errorFile      *os.File
	resultFile     *os.File
	combinedFile   *os.File
	infoLogger     *log.Logger
	errorLogger    *log.Logger
	resultLogger   *log.Logger
	combinedLogger *log.Logger
	mutex          sync.Mutex
}

// NewLogger creates a new logger with files based on configuration
func NewLogger(config Config) (*Logger, error) {
	// Generate filename based on configuration
	timestamp := time.Now().Format("20060102_150405")

	// Create duration string
	var durationStr string
	if config.RunMinutes > 0 {
		durationStr = fmt.Sprintf("%dm", config.RunMinutes)
	} else if config.Duration > 0 {
		durationStr = fmt.Sprintf("%.0fs", config.Duration.Seconds())
	} else {
		durationStr = "unlimited"
	}

	// Sanitize server address for filename (replace colons and other invalid chars)
	sanitizedServer := strings.ReplaceAll(config.ServerAddress, ":", "_")
	sanitizedServer = strings.ReplaceAll(sanitizedServer, "/", "_")
	sanitizedServer = strings.ReplaceAll(sanitizedServer, "\\", "_")

	filename := fmt.Sprintf("stress_test_%s_accounts%d_rate%d_duration%s_%s.log",
		timestamp, config.AccountCount, config.TxPerSecond, durationStr, sanitizedServer)

	// Create logs directory if it doesn't exist
	logDir := "reports/log_reports"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %v", err)
	}

	// Create ONLY combined report log file
	reportPath := filepath.Join(logDir, fmt.Sprintf("report_%s", filename))

	// Log the file path for debugging
	logx.Info("STRESS", fmt.Sprintf("Creating log file: %s", reportPath))

	combinedFile, err := os.OpenFile(reportPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create combined report log file '%s': %v", reportPath, err)
	}

	// Verify file was created successfully
	logx.Info("STRESS", "Successfully created report log file")

	// Create logger
	var infoLogger *log.Logger
	var errorLogger *log.Logger
	var resultLogger *log.Logger
	combinedLogger := log.New(combinedFile, "", log.LstdFlags|log.Lmicroseconds)

	// Write initial configuration to combined report
	combinedLogger.Printf("[RESULT] === STRESS TEST CONFIGURATION ===")
	combinedLogger.Printf("[RESULT] Server Address: %s", config.ServerAddress)
	combinedLogger.Printf("[RESULT] Use Private Key for Transfers: %t", config.TransferByPrivateKey)
	combinedLogger.Printf("[RESULT] Account Count: %d", config.AccountCount)
	combinedLogger.Printf("[RESULT] Transactions Per Second: %d", config.TxPerSecond)
	combinedLogger.Printf("[RESULT] Fund Amount: %d", config.FundAmount)
	combinedLogger.Printf("[RESULT] Transfer Amount: %d", config.TransferAmount)
	combinedLogger.Printf("[RESULT] Duration: %v", config.Duration)
	combinedLogger.Printf("[RESULT] Total Transactions: %d", config.TotalTransactions)
	combinedLogger.Printf("[RESULT] Transactions Err Per Second: %d", config.ErrBalance+config.ErrNonce+config.ErrDuplicate+config.ErrRequest)
	combinedLogger.Printf("[RESULT] Duplicate Err Per Second: %d", config.ErrDuplicate)
	combinedLogger.Printf("[RESULT] Balance Err Per Second: %d", config.ErrBalance)
	combinedLogger.Printf("[RESULT] Nonce Err Per Second: %d", config.ErrNonce)
	combinedLogger.Printf("[RESULT] Request Err Per Second: %d", config.ErrRequest)
	combinedLogger.Printf("[RESULT] Test Start Time: %s", time.Now().Format("2006-01-02 15:04:05"))
	combinedLogger.Printf("[RESULT] =================================")

	// Force flush to ensure data is written
	combinedFile.Sync()

	return &Logger{
		infoFile:       nil,
		errorFile:      nil,
		resultFile:     nil,
		combinedFile:   combinedFile,
		infoLogger:     infoLogger,
		errorLogger:    errorLogger,
		resultLogger:   resultLogger,
		combinedLogger: combinedLogger,
	}, nil
}

// LogInfo logs info messages to both console and file
func (l *Logger) LogInfo(format string, v ...interface{}) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	message := fmt.Sprintf(format, v...)

	// Console via logx
	logx.Info("STRESS", message)

	// Also write to combined report
	if l.combinedLogger != nil && l.combinedFile != nil {
		l.combinedLogger.Printf("[INFO] %s", message)
		l.combinedFile.Sync()
	}
}

// LogError logs error messages to both console and file
func (l *Logger) LogError(format string, v ...interface{}) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	message := fmt.Sprintf(format, v...)

	// Console via logx
	logx.Error("STRESS", message)

	// Also write to combined report
	if l.combinedLogger != nil && l.combinedFile != nil {
		l.combinedLogger.Printf("[ERROR] %s", message)
		l.combinedFile.Sync()
	}
}

// LogResult logs result messages to both console and file
func (l *Logger) LogResult(format string, v ...interface{}) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	message := fmt.Sprintf(format, v...)

	// Console via logx (category RESULT)
	logx.Info("RESULT", message)

	// Also write to combined report
	if l.combinedLogger != nil && l.combinedFile != nil {
		l.combinedLogger.Printf("[RESULT] %s", message)
		l.combinedFile.Sync()
	}
}

// collectSystemMetrics collects current system metrics
func collectSystemMetrics() (*SystemMetrics, error) {
	metrics := &SystemMetrics{
		Timestamp: time.Now(),
	}

	// CPU usage
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU metrics: %v", err)
	}
	if len(cpuPercent) > 0 {
		metrics.CPUPercent = cpuPercent[0]
	}

	// Memory usage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory metrics: %v", err)
	}
	metrics.MemoryPercent = memInfo.UsedPercent

	// Disk I/O
	diskStats, err := disk.IOCounters()
	if err != nil {
		return nil, fmt.Errorf("failed to get disk metrics: %v", err)
	}

	// Sum up all disk I/O
	for _, stat := range diskStats {
		metrics.DiskRead += stat.ReadBytes
		metrics.DiskWrite += stat.WriteBytes
	}

	// Network I/O
	netStats, err := net.IOCounters(false)
	if err != nil {
		return nil, fmt.Errorf("failed to get network metrics: %v", err)
	}

	if len(netStats) > 0 {
		metrics.NetworkRx = netStats[0].BytesRecv
		metrics.NetworkTx = netStats[0].BytesSent
	}

	return metrics, nil
}

// formatBytes formats bytes into human readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// LogRealTimeMetrics logs real-time metrics every 10 seconds
func (l *Logger) LogRealTimeMetrics(totalTxs, successTxs, failedTxs int64, testStartTime time.Time, config Config) {
	testDuration := time.Since(testStartTime)
	actualRate := float64(totalTxs) / testDuration.Seconds()
	successRate := float64(successTxs) / float64(totalTxs) * 100
	if totalTxs == 0 {
		successRate = 0
	}

	// Collect system metrics
	sysMetrics, err := collectSystemMetrics()
	if err != nil {
		l.LogError("Failed to collect system metrics: %v", err)
		sysMetrics = &SystemMetrics{}
	}

	// Show remaining time if using minutes option
	timeInfo := ""
	if config.Duration > 0 {
		elapsed := time.Since(testStartTime)
		remaining := config.Duration - elapsed
		if remaining > 0 {
			timeInfo = fmt.Sprintf(" | Time: %v elapsed, %v remaining",
				elapsed.Round(time.Second), remaining.Round(time.Second))
		} else {
			timeInfo = fmt.Sprintf(" | Time: %v elapsed (test should stop soon)",
				elapsed.Round(time.Second))
		}
	}

	// Create system metrics string
	sysInfo := fmt.Sprintf(" | CPU: %.1f%%, RAM: %.1f%%, Disk: R%s/W%s, Net: Rx%s/Tx%s",
		sysMetrics.CPUPercent, sysMetrics.MemoryPercent,
		formatBytes(sysMetrics.DiskRead), formatBytes(sysMetrics.DiskWrite),
		formatBytes(sysMetrics.NetworkRx), formatBytes(sysMetrics.NetworkTx))

	message := fmt.Sprintf("REAL-TIME METRICS [%s] | Txs: %d sent, %d success, %d failed | Rate: %.2f tx/s, %.1f%% success%v%v",
		time.Now().Format("15:04:05"), totalTxs, successTxs, failedTxs, actualRate, successRate, timeInfo, sysInfo)

	l.LogInfo("%s", message)
}

// LogFinalStats logs final test statistics
func (l *Logger) LogFinalStats(totalTxs, successTxs, failedTxs int64, testStartTime time.Time, config Config, endTime time.Time) {
	testDuration := endTime.Sub(testStartTime)
	actualRate := float64(totalTxs) / testDuration.Seconds()
	successRate := float64(successTxs) / float64(totalTxs) * 100

	// Collect final system metrics
	sysMetrics, err := collectSystemMetrics()
	if err != nil {
		l.LogError("Failed to collect final system metrics: %v", err)
		sysMetrics = &SystemMetrics{}
	}

	l.LogResult("=== FINAL TEST STATISTICS ===")
	l.LogResult("Test Duration: %v", testDuration.Round(time.Second))
	l.LogResult("Total Transactions Sent: %d", totalTxs)
	l.LogResult("Successful Transactions: %d", successTxs)
	l.LogResult("Failed Transactions: %d", failedTxs)
	l.LogResult("Actual Rate: %.2f tx/s", actualRate)
	l.LogResult("Success Rate: %.2f%%", successRate)
	l.LogResult("Accounts Used: %d", config.AccountCount)
	l.LogResult("=== SYSTEM METRICS ===")
	l.LogResult("CPU Usage: %.1f%%", sysMetrics.CPUPercent)
	l.LogResult("Memory Usage: %.1f%%", sysMetrics.MemoryPercent)
	l.LogResult("Disk Read: %s", formatBytes(sysMetrics.DiskRead))
	l.LogResult("Disk Write: %s", formatBytes(sysMetrics.DiskWrite))
	l.LogResult("Network Received: %s", formatBytes(sysMetrics.NetworkRx))
	l.LogResult("Network Transmitted: %s", formatBytes(sysMetrics.NetworkTx))
	l.LogResult("Test End Time: %s", time.Now().Format("2006-01-02 15:04:05"))
	l.LogResult("=============================")
}

// Close closes all log files
func (l *Logger) Close() {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.combinedFile != nil {
		l.combinedFile.Close()
	}
}
