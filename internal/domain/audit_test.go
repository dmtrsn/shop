package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var auditStaff = StaffUser{ID: "stf_" + strings.Repeat("a", 32)}

func TestAuditActionNames(t *testing.T) {
	for _, valid := range []AuditAction{"product.update", "order.status_change", "a.b", AuditAction(strings.Repeat("a", 32) + "." + strings.Repeat("b", 32))} {
		if !valid.Valid() {
			t.Errorf("%q rejected", valid)
		}
	}
	for _, invalid := range []AuditAction{"", "product", "product.", ".update", "Product.update", "product.Update", "product.update.extra",
		"product-x.update", "1product.update", "product.up date", "_product.update", AuditAction(strings.Repeat("a", 33) + ".update"), "product.update\n"} {
		if invalid.Valid() {
			t.Errorf("%q accepted", invalid)
		}
	}
}

func TestAuditTargets(t *testing.T) {
	for _, valid := range []AuditTarget{{"product", "p01"}, {"order", "ord_0123abcd"}, {"order_item", "usr-demo:1.2"}, {"product", strings.Repeat("x", 128)}} {
		if !valid.Valid() {
			t.Errorf("%+v rejected", valid)
		}
	}
	for _, invalid := range []AuditTarget{{}, {"product", ""}, {"", "p01"}, {"Product", "p01"}, {"product", "p 01"}, {"product", "p01/../x"},
		{"product", "пр01"}, {"product", strings.Repeat("x", 129)}, {"product", "p01\x00"}, {strings.Repeat("a", 33), "p01"}} {
		if invalid.Valid() {
			t.Errorf("%+v accepted", invalid)
		}
	}
}

func TestStaffMutationTakesTheActorFromAStaffPrincipal(t *testing.T) {
	mutation, err := NewStaffMutation(auditStaff, "product.update", "0123456789abcdef")
	if err != nil || mutation.Actor() != auditStaff.ID || mutation.Action() != "product.update" || mutation.RequestID() != "0123456789abcdef" || !mutation.Valid() {
		t.Fatalf("%+v %v", mutation, err)
	}
	if mutation, err := NewStaffMutation(auditStaff, "product.update", ""); err != nil || mutation.RequestID() != "" {
		t.Fatal("a mutation outside HTTP has no request ID")
	}
	// Anything that is not a staff identifier, customer IDs included, is not
	// an actor.
	for _, id := range []string{"", "usr-demo", "usr_" + strings.Repeat("a", 32), "stf_" + strings.Repeat("A", 32), "stf_x", " stf_" + strings.Repeat("a", 32)} {
		if _, err := NewStaffMutation(StaffUser{ID: id}, "product.update", ""); !errors.Is(err, ErrForbidden) {
			t.Errorf("actor %q: %v", id, err)
		}
	}
	if _, err := NewStaffMutation(auditStaff, "Product.Update", ""); err == nil || errors.Is(err, ErrValidation) {
		t.Fatal("an invalid action is a programming error, not client input")
	}
	if (StaffMutation{}).Valid() {
		t.Fatal("the zero mutation is valid")
	}
}

func TestStaffMutationBoundsTheRequestID(t *testing.T) {
	for _, valid := range []string{"0123456789abcdef", "8f14e45f-ceea-467a-9575-9a7d0f8fd5c2", "edge:req.1_a", strings.Repeat("r", 128)} {
		if _, err := NewStaffMutation(auditStaff, "product.update", valid); err != nil {
			t.Errorf("%q: %v", valid, err)
		}
	}
	for _, invalid := range []string{strings.Repeat("r", 129), "with space", "line\nbreak", "ид", "a/b", `"quoted"`} {
		var validation *ValidationError
		if _, err := NewStaffMutation(auditStaff, "product.update", invalid); !errors.As(err, &validation) || validation.Fields[0].Field != "X-Request-ID" {
			t.Errorf("%q: %v", invalid, err)
		}
	}
}

func TestStaffAuditEventComesFromTheMutation(t *testing.T) {
	mutation, err := NewStaffMutation(auditStaff, "order.cancel", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := mutation.Event("aud_1", AuditTarget{"order", "ord_1"})
	want := StaffAuditEvent{ID: "aud_1", StaffUserID: auditStaff.ID, Action: "order.cancel", EntityType: "order", EntityID: "ord_1", RequestID: "req-1"}
	if err != nil || event != want {
		t.Fatalf("%+v %v", event, err)
	}
	if _, err := mutation.Event("aud_1", AuditTarget{"order", ""}); err == nil {
		t.Fatal("an event without a target")
	}
	if _, err := (StaffMutation{}).Event("aud_1", AuditTarget{"order", "ord_1"}); err == nil {
		t.Fatal("an event without an authorized mutation")
	}
}

// The audit types have a fixed set of scalar fields: no map, interface, byte
// slice or nested value can carry a request dump, credential, token or
// cookie. Adding a field is a deliberate change of this test and of ADR 012.
func TestAuditTypesHaveNoRoomForSecrets(t *testing.T) {
	stringType, actionType, timeType := reflect.TypeOf(""), reflect.TypeOf(AuditAction("")), reflect.TypeOf(time.Time{})
	for _, tc := range []struct {
		value  any
		fields map[string]reflect.Type
	}{
		{StaffAuditEvent{}, map[string]reflect.Type{"ID": stringType, "StaffUserID": stringType, "Action": actionType, "EntityType": stringType, "EntityID": stringType, "RequestID": stringType, "CreatedAt": timeType}},
		{StaffMutation{}, map[string]reflect.Type{"actor": stringType, "action": actionType, "requestID": stringType}},
		{AuditTarget{}, map[string]reflect.Type{"Type": stringType, "ID": stringType}},
	} {
		typ := reflect.TypeOf(tc.value)
		got := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			got[typ.Field(i).Name] = typ.Field(i).Type
		}
		if !reflect.DeepEqual(got, tc.fields) {
			t.Errorf("%s fields changed: %v", typ.Name(), got)
		}
	}
}
