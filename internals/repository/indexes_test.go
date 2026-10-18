package repository

import (
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestIsDuplicateKeyError(t *testing.T) {
	duplicate := mongo.CommandError{Code: 11000, Message: "duplicate key"}
	if !isDuplicateKeyError(duplicate) {
		t.Fatal("expected duplicate-key index build error to be recognized")
	}
	if !errors.Is(historicalDuplicateEmailWarning(duplicate), ErrHistoricalDuplicateEmails) {
		t.Fatal("expected duplicate-data warning to preserve its sentinel")
	}
	if isDuplicateKeyError(errors.New("network unavailable")) {
		t.Fatal("non-duplicate index errors must remain fatal")
	}
}
