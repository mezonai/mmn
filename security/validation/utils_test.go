package validation

import (
	"crypto/ed25519"
	"fmt"
	"testing"

	"github.com/mezonai/mmn/errors"
	"github.com/mr-tron/base58"
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

func TestValidateTxAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{
			name: "valid ed25519 public key",
			address: func() string {
				pub, _, _ := ed25519.GenerateKey(nil)
				return base58.Encode(pub)
			}(),
			want: true,
		},
		{
			name:    "invalid base58 string",
			address: "%%%not-base58%%%",
			want:    false,
		},
		{
			name: "invalid curve point",
			address: func() string {
				invalid := make([]byte, 32)
				for i := 0; i < 32; i++ {
					invalid[i] = byte(i*7 + 3)
				}
				return base58.Encode(invalid)
			}(),
			want: false,
		},
		{
			name: "wrong length (not 32 bytes)",
			address: func() string {
				return base58.Encode([]byte{1, 2, 3})
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateTxAddress(tt.address)
			if got != tt.want {
				t.Fatalf("ValidateTxAddress(%q) = %v, want %v", tt.address, got, tt.want)
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
