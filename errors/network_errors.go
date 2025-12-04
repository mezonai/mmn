package errors

import (
	"github.com/mezonai/mmn/jsonx"
)

// NetworkErrorCode represents standardized error codes for network operations
type NetworkErrorCode string

const (
	// General errors
	ErrCodeInternal NetworkErrorCode = "internal_error"

	// Validation errors
	ErrCodeInvalidRequest     = "invalid_request"
	ErrCodeInvalidTransaction = "invalid_transaction"
	ErrCodeInvalidSignature   = "invalid_signature"
	ErrCodeInvalidAddress     = "invalid_address"
	ErrCodeInvalidAmount      = "invalid_amount"
	ErrCodeInvalidNonce       = "invalid_nonce"

	// Business logic errors
	ErrCodeTransactionNotFound  = "transaction_not_found"
	ErrCodeAccountNotFound      = "account_not_found"
	ErrCodeInsufficientFunds    = "insufficient_funds"
	ErrCodeNonceTooLow          = "nonce_too_low"
	ErrCodeNonceTooHigh         = "nonce_too_high"
	ErrCodeDuplicateTransaction = "duplicate_transaction"

	// System errors
	ErrCodeMempoolFull = "mempool_full"
	ErrCodeRateLimited = "rate_limited"
)

// NetworkError represents a standardized network error
type NetworkError struct {
	Code    NetworkErrorCode `json:"code"`
	Message string           `json:"message"`
}

// Error implements the error interface
func (e *NetworkError) Error() string {
	err, _ := jsonx.Marshal(NetworkError{
		Code:    e.Code,
		Message: e.Message,
	})
	return string(err)
}

// Error message constants - user-friendly and concise
const (
	ErrMsgInvalidRequest                      = "Request format is invalid"
	ErrMsgInvalidTransaction                  = "Transaction data is invalid"
	ErrMsgInvalidSignature                    = "Transaction signature is invalid"
	ErrMsgInvalidAddress                      = "Wallet address is invalid"
	ErrMsgInvalidAmount                       = "Amount is invalid or zero"
	ErrMsgInvalidNonce                        = "Transaction nonce is invalid"
	ErrMsgInsufficientFunds                   = "Not enough balance in your wallet"
	ErrMsgNonceTooLow                         = "Transaction nonce is too low"
	ErrMsgNonceTooHigh                        = "Transaction nonce is too high"
	ErrMsgTransactionNotFound                 = "Transaction could not be found"
	ErrMsgAccountNotFound                     = "Account does not exist"
	ErrMsgMempoolFull                         = "Network is busy, please try again"
	ErrMsgDuplicateTransaction                = "This transaction already exists"
	ErrMsgInternal                            = "Server error, please try again"
	ErrMsgRateLimited                         = "Too many requests, please slow down"
	ErrMsgRequestBodyTooLarge                 = "Request body exceeds maximum allowed size (%d bytes)"
	ErrMsgShortTextTooLong                    = "Short text length exceeds maximum (%d) for field '%s'"
	ErrMsgLongTextTooLong                     = "Long text length exceeds maximum (%d) for field '%s'"
	ErrMsgInvalidCharacters                   = "Field '%s' contains invalid characters"
	ErrMsgInvalidDonationCampaignFeed         = "Donation campaign feed data is invalid"
	ErrMsgDonationCampaignFeedParentNotFound  = "Parent donation campaign feed not found"
	ErrMsgDonationCampaignFeedRootNotFound    = "Root donation campaign feed not found"
	ErrMsgDonationCampaignFeedVersionConflict = "Parent donation campaign feed is not the latest version"
)

// NewError creates a new NetworkError and returns it as error interface
func NewError(code NetworkErrorCode, message string) error {
	return &NetworkError{
		Code:    code,
		Message: message,
	}
}
