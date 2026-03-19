package postgres

import (
	"errors"

	"aichain/internal/protocol"
)

func classifyErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrValidation):
		return protocol.ReceiptCodeValidation
	case errors.Is(err, ErrUnauthorized):
		return protocol.ReceiptCodeUnauthorized
	case errors.Is(err, ErrInvalidNonce):
		return protocol.ReceiptCodeInvalidNonce
	case errors.Is(err, ErrInsufficientBalance):
		return protocol.ReceiptCodeInsufficientBalance
	case errors.Is(err, ErrNotFound):
		return protocol.ReceiptCodeNotFound
	case errors.Is(err, ErrDuplicateSubmission), errors.Is(err, ErrDuplicateTransaction):
		return protocol.ReceiptCodeConflict
	default:
		return protocol.ReceiptCodeInternal
	}
}
