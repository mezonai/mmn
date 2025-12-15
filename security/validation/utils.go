package validation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"filippo.io/edwards25519"
	"github.com/mezonai/mmn/errors"
	"github.com/mr-tron/base58"
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

	if utf8.RuneCountInString(normalized) > MaxLongTextLength {
		return errors.NewError(
			errors.ErrCodeInvalidRequest,
			fmt.Sprintf(errors.ErrMsgLongTextTooLong, MaxLongTextLength, fieldName),
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

func ValidateTxAddress(addr string) bool {
	pubKey, err := base58.Decode(addr)
	if err != nil {
		return false
	}

	if len(pubKey) != addressDecodedExpectedLength {
		return false
	}

	if _, err = new(edwards25519.Point).SetBytes(pubKey); err != nil {
		return false
	}

	return true
}

func ShouldValidateAddress(extraInfoType string) bool {
	_, needValidate := TxExtraInfoTypeNeedValidateAddress[extraInfoType]
	return needValidate
}
