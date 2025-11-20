package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	TEST_NAME             = "MMN Load Test Report"
	RUNNER                = "load-txs.go"
	HTML_REPORT_TEMPLATE  = "templates/report_template.html"
	HTML_REPORT_DIR       = "reports/html_reports"
	HTML_REPORT_FILE_NAME = "report_stress_test_%s.html"
)

type TestMeta struct {
	TestName        string     `json:"test_name"`
	Runner          string     `json:"runner"`
	StartTime       string     `json:"start_time"`
	EndTime         string     `json:"end_time"`
	DurationSeconds int64      `json:"duration_seconds"`
	Config          TestConfig `json:"config"`
}

type TestConfig struct {
	ServerAddress     string `json:"server_address"`
	AccountCount      int    `json:"account_count"`
	TargetTPS         int    `json:"target_tps"`
	FundAmount        uint64 `json:"fund_amount"`
	TransferAmount    uint64 `json:"transfer_amount"`
	DurationSeconds   int64  `json:"duration_seconds"`
	TotalTransactions int64  `json:"total_transactions"`
}

type TestSummary struct {
	TotalSent   int64   `json:"total_sent"`
	Success     int64   `json:"success"`
	Failed      int64   `json:"failed"`
	AvgTPS      float64 `json:"avg_tps"`
	SuccessRate float64 `json:"success_rate"`
}

type TestSeries struct {
	TPS    [][]int64        `json:"tps"`
	Errors map[string]int64 `json:"errors"`
}

type TestReport struct {
	Meta     TestMeta    `json:"meta"`
	Summary  TestSummary `json:"summary"`
	Series   TestSeries  `json:"series"`
	Snapshot []Snapshot  `json:"snapshot"`
}

func GenerateAndSaveReports(
	config Config,
	startTime time.Time,
	endTime time.Time,
	totalTxs int64,
	successTxs int64,
	failedTxs int64,
	tpsHistory [][]int64,
	errorCounts map[string]int64,
	snapshots []Snapshot,
	logger *Logger,
) {
	testDuration := endTime.Sub(startTime)
	avgTPS := float64(0)
	if testDuration.Seconds() > 0 {
		avgTPS = float64(totalTxs) / testDuration.Seconds()
	}

	successRate := float64(0)
	if totalTxs > 0 {
		successRate = float64(successTxs) / float64(totalTxs) * 100
	}

	// Create report structure
	report := TestReport{
		Meta: TestMeta{
			TestName:        TEST_NAME,
			Runner:          RUNNER,
			StartTime:       startTime.Format(time.RFC3339),
			EndTime:         endTime.Format(time.RFC3339),
			DurationSeconds: int64(testDuration.Seconds()),
			Config: TestConfig{
				ServerAddress:     config.ServerAddress,
				AccountCount:      config.AccountCount,
				TargetTPS:         config.TxPerSecond,
				FundAmount:        config.FundAmount,
				TransferAmount:    config.TransferAmount,
				DurationSeconds:   config.Duration.Milliseconds() / 1000,
				TotalTransactions: config.TotalTransactions,
			},
		},
		Summary: TestSummary{
			TotalSent:   totalTxs,
			Success:     successTxs,
			Failed:      failedTxs,
			AvgTPS:      avgTPS,
			SuccessRate: successRate,
		},
		Series: TestSeries{
			TPS:    tpsHistory,
			Errors: errorCounts,
		},
		Snapshot: snapshots,
	}

	// Create JSON Data
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		logger.LogError("Failed to marshal JSON report: %v", err)
		return
	}

	templateFile := filepath.Join(HTML_REPORT_TEMPLATE)

	// Check template file exists
	if _, err := os.Stat(templateFile); os.IsNotExist(err) {
		logger.LogError("Template file not found: %s", templateFile)
		return
	}

	// Read template
	templateData, err := os.ReadFile(templateFile)
	if err != nil {
		logger.LogError("Failed to read HTML template: %v", err)
		return
	}

	// Replace placeholder with JSON data
	htmlContent := string(templateData)
	pattern := `window\.__EMBEDDED_REPORT = \{[\s\S]*?\}; // \{\{EMBEDDED_DATA\}\}`
	replacement := fmt.Sprintf("window.__EMBEDDED_REPORT = %s; // {{EMBEDDED_DATA}}", string(jsonData))
	re := regexp.MustCompile(pattern)
	newHTML := re.ReplaceAllString(htmlContent, replacement)

	// Ensure HTML reports directory exists
	if err := os.MkdirAll(HTML_REPORT_DIR, 0755); err != nil {
		logger.LogError("Failed to create html_reports directory: %v", err)
		return
	}

	// Write HTML file
	timestamp := endTime.Format("20060102_150405")
	htmlFilename := fmt.Sprintf(HTML_REPORT_FILE_NAME, timestamp)
	htmlPath := filepath.Join(HTML_REPORT_DIR, htmlFilename)
	if err := os.WriteFile(htmlPath, []byte(newHTML), 0644); err != nil {
		logger.LogError("Failed to write HTML report: %v", err)
		return
	}

	logger.LogResult("HTML report saved: %s", htmlPath)
}
