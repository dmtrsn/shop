package application

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The dummy hash must cost the same as provisioned customer passwords so an
// unknown email takes as long as a wrong password.
func TestDummyPasswordHashMatchesCustomerCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyPasswordHash))
	if err != nil || cost != CustomerPasswordCost {
		t.Fatalf("dummy hash cost=%d err=%v", cost, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte("")) == nil {
		t.Fatal("dummy hash must not verify an empty password")
	}
}
