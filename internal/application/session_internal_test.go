package application

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The dummy hash must cost the same as the seeded credential (cost 10) so an
// unknown email takes as long as a wrong password.
func TestDummyPasswordHashMatchesSeededCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyPasswordHash))
	if err != nil || cost != 10 {
		t.Fatalf("dummy hash cost=%d err=%v", cost, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte("")) == nil {
		t.Fatal("dummy hash must not verify an empty password")
	}
}
