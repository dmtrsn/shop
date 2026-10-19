package postgres

import (
	"reflect"
	"regexp"
	"testing"
)

func TestAuditIDsAreRandomAndNamespaced(t *testing.T) {
	pattern := regexp.MustCompile(`^aud_[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, err := newAuditID()
		if err != nil || !pattern.MatchString(id) || seen[id] {
			t.Fatalf("%q %v", id, err)
		}
		seen[id] = true
	}
}

// A mutation can run statements but cannot end or nest its transaction, so
// it cannot commit before its audit event exists.
func TestStaffTxExposesStatementsOnly(t *testing.T) {
	typ := reflect.TypeOf(StaffTx{})
	var methods []string
	for i := 0; i < typ.NumMethod(); i++ {
		methods = append(methods, typ.Method(i).Name)
	}
	if !reflect.DeepEqual(methods, []string{"Exec", "Query", "QueryRow"}) {
		t.Fatalf("StaffTx methods: %v", methods)
	}
	if typ.NumField() != 1 || typ.Field(0).IsExported() {
		t.Fatal("StaffTx must not expose its transaction")
	}
}
