package validation

import (
	"fmt"
	"testing"

	"github.com/mezonai/mmn/errors"
)

func TestValidateShortTextLength(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		value     string
		wantErr   bool
		wantCode  errors.NetworkErrorCode
		wantMsg   string
	}{
		{
			name:      "valid",
			fieldName: "valid_field",
			value:     "hello",
			wantErr:   false,
		},
		{
			name:      "empty string",
			fieldName: "empty_field",
			value:     "",
			wantErr:   false,
		},
		{
			name:      "json string",
			fieldName: "json_field",
			value:     "{\"key\": \"value\"}",
			wantErr:   false,
		},
		{
			name:      "too long",
			fieldName: "too_long_field",
			value:     makeString(MaxShortTextLength + 1),
			wantErr:   true,
			wantCode:  errors.ErrCodeInvalidRequest,
			wantMsg:   fmt.Sprintf(errors.ErrMsgShortTextTooLong, MaxShortTextLength, "too_long_field"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateShortTextLength(tt.fieldName, tt.value)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			netErr, ok := err.(*errors.NetworkError)
			if !ok {
				t.Fatalf("expected NetworkError, got %T", err)
			}

			if netErr.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s", tt.wantCode, netErr.Code)
			}

			if netErr.Message != tt.wantMsg {
				t.Fatalf("expected message %q, got %q", tt.wantMsg, netErr.Message)
			}
		})
	}
}

func TestValidateLongTextLength(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		value     string
		wantErr   bool
		wantCode  errors.NetworkErrorCode
		wantMsg   string
	}{
		{
			name:      "valid",
			fieldName: "content",
			value:     "this is ok",
			wantErr:   false,
		},
		{
			name:      "empty string",
			fieldName: "empty_field",
			value:     "",
			wantErr:   false,
		},
		{
			name:      "json string",
			fieldName: "json_field",
			value:     "{\"key\": \"value\"}",
			wantErr:   false,
		},
		{
			name:      "injection pattern",
			fieldName: "injection_field",
			value:     "test {{ alert(1) }}",
			wantErr:   true,
			wantCode:  errors.ErrCodeInvalidRequest,
			wantMsg:   fmt.Sprintf(errors.ErrMsgInvalidCharacters, "injection_field"),
		},
		{
			name:      "too long",
			fieldName: "too_long_field",
			value:     makeString(MaxLongTextLength + 1),
			wantErr:   true,
			wantCode:  errors.ErrCodeInvalidRequest,
			wantMsg:   fmt.Sprintf(errors.ErrMsgLongTextTooLong, MaxLongTextLength, "too_long_field"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLongTextLength(tt.fieldName, tt.value)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			netErr, ok := err.(*errors.NetworkError)
			if !ok {
				t.Fatalf("expected NetworkError, got %T", err)
			}

			if netErr.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s", tt.wantCode, netErr.Code)
			}

			if netErr.Message != tt.wantMsg {
				t.Fatalf("expected message %q, got %q", tt.wantMsg, netErr.Message)
			}
		})
	}
}

// helper
func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
