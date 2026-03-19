package postgres

import (
	"fmt"
	"testing"

	"aichain/internal/protocol"
)

func TestClassifyErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "validation", err: fmt.Errorf("%w: bad payload", ErrValidation), want: protocol.ReceiptCodeValidation},
		{name: "unauthorized", err: fmt.Errorf("%w: bad signature", ErrUnauthorized), want: protocol.ReceiptCodeUnauthorized},
		{name: "nonce", err: fmt.Errorf("%w: expected nonce 2", ErrInvalidNonce), want: protocol.ReceiptCodeInvalidNonce},
		{name: "balance", err: ErrInsufficientBalance, want: protocol.ReceiptCodeInsufficientBalance},
		{name: "not found", err: ErrNotFound, want: protocol.ReceiptCodeNotFound},
		{name: "conflict", err: ErrDuplicateTransaction, want: protocol.ReceiptCodeConflict},
		{name: "internal", err: fmt.Errorf("boom"), want: protocol.ReceiptCodeInternal},
	}

	for _, test := range tests {
		if got := classifyErrorCode(test.err); got != test.want {
			t.Fatalf("%s: expected %s, got %s", test.name, test.want, got)
		}
	}
}
