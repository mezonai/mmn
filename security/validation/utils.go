package validation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mezonai/mmn/errors"
	"golang.org/x/text/unicode/norm"
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
func ValidateShortTextLength(fieldName, fieldValue string) error {
	normalized := norm.NFC.String(fieldValue)

	if utf8.RuneCountInString(normalized) > MaxShortTextLength {
		return errors.NewError(
			errors.ErrCodeInvalidRequest,
			fmt.Sprintf(errors.ErrMsgShortTextTooLong, MaxShortTextLength, fieldName),
		)
	}
	return nil
}

// ValidateLongTextLength validates long text field length
func ValidateLongTextLength(fieldName, fieldValue string) error {
	normalized := norm.NFC.String(fieldValue)

	maxLength := MaxLongTextLength
	if _, ok := ExtendedTextFields[fieldName]; ok {
		maxLength = MaxExtendedTextLength
	}

	if utf8.RuneCountInString(normalized) > maxLength {
		return errors.NewError(
			errors.ErrCodeInvalidRequest,
			fmt.Sprintf(errors.ErrMsgLongTextTooLong, maxLength, fieldName),
		)
	}

	if InjectionRegexp.MatchString(normalized) {
		return errors.NewError(
			errors.ErrCodeInvalidRequest,
			fmt.Sprintf(errors.ErrMsgInvalidCharacters, fieldName),
		)
	}

	return nil
}
