package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"

	"github.com/mezonai/mmn/logx"
)

func GetUsers(filePath string) ([]map[string]interface{}, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV file: %v", err)
	}

	var users []map[string]interface{}

	// Skip header row if it exists
	startIndex := 0
	if len(records) > 0 && (records[0][0] == "id" || records[0][0] == "ID") {
		startIndex = 1
	}

	for i := startIndex; i < len(records); i++ {
		record := records[i]
		if len(record) < 3 {
			continue // Skip incomplete rows
		}

		id, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			logx.Warn("CSV_PARSER", fmt.Sprintf("Failed to parse id '%s' in row %d: %v", record[0], i+1, err))
			continue
		}

		username := record[1]

		wallet, err := strconv.ParseInt(record[2], 10, 64)
		if err != nil {
			logx.Warn("CSV_PARSER", fmt.Sprintf("Failed to parse wallet '%s' in row %d: %v", record[2], i+1, err))
			continue
		}

		// Only include users with valid wallet > 0
		if wallet > 0 {
			users = append(users, map[string]interface{}{
				"id":      id,
				"name":    username,
				"balance": wallet,
			})
		}
	}

	return users, nil
}
