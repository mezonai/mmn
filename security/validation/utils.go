package validation

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var InjectionRegexp = BuildInjectionPatterns()

// BuildInjectionPatterns builds regexp for injection detection (case-insensitive)
func BuildInjectionPatterns() *regexp.Regexp {
	parts := make([]string, 0, len(InjectionPatterns))
	for _, pattern := range InjectionPatterns {
		pNorm := norm.NFC.String(pattern)
		parts = append(parts, regexp.QuoteMeta(pNorm))
	}
	// (?i) for case-insensitive
	return regexp.MustCompile("(?i)" + strings.Join(parts, "|"))
}

// ValidateShortTextLength validates short text field length
func ValidateShortTextLength(fieldName string, fieldValue string) error {
	normalized := norm.NFC.String(fieldValue)

	if utf8.RuneCountInString(normalized) > MaxShortTextLength {
		return status.Errorf(codes.InvalidArgument,
			"field %s: short text length exceeds maximum of %d", fieldName, MaxShortTextLength)
	}
	return nil
}

// ValidateLongTextLength validates long text field length
func ValidateLongTextLength(fieldName string, fieldValue string) error {
	normalized := norm.NFC.String(fieldValue)

	if utf8.RuneCountInString(normalized) > MaxLongTextLength {
		return status.Errorf(codes.InvalidArgument,
			"field %s: long text length exceeds maximum of %d", fieldName, MaxLongTextLength)
	}

	if InjectionRegexp.MatchString(normalized) {
		return status.Errorf(codes.InvalidArgument,
			"field %s: contains injection pattern", fieldName)
	}

	return nil
}
