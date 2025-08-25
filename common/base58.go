package common

import (
	"encoding/hex"
	"fmt"

	"github.com/mr-tron/base58"
)

// EncodeToBase58 encodes a hex string to base58
func EncodeToBase58(hexStr string) (string, error) {
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}

	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode hex string: %w", err)
	}

	return base58.Encode(bytes), nil
}

// DecodeFromBase58 decodes a base58 string to hex
func DecodeFromBase58(base58Str string) (string, error) {
	bytes, err := base58.Decode(base58Str)
	if err != nil {
		return "", fmt.Errorf("failed to decode base58 string: %w", err)
	}
	if len(bytes) == 0 {
		return "", fmt.Errorf("failed to decode base58 string")
	}

	return hex.EncodeToString(bytes), nil
}

// EncodeBytesToBase58 encodes bytes directly to base58
func EncodeBytesToBase58(bytes []byte) string {
	return base58.Encode(bytes)
}

// DecodeBase58ToBytes decodes base58 string to bytes
func DecodeBase58ToBytes(base58Str string) ([]byte, error) {
	bytes, err := base58.Decode(base58Str)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base58 string: %w", err)
	}
	return bytes, nil
}

// IsValidBase58 checks if a string is valid base58
func IsValidBase58(str string) bool {
	decoded, err := base58.Decode(str)
	return err == nil && len(decoded) > 0
}
