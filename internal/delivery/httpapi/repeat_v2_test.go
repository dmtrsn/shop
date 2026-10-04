package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
	"sweetshop/tests/contract"
)

const repeatPath = "/api/v2/orders/ord-2026-002/repeat/"

func repeatRequest(h *harness, token, body string, keys ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", repeatPath+"commit", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	for _, key := range keys {
		r.Header.Add("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w
}

func TestRepeatV2StrictHTTPAndReplay(t *testing.T) {
	store := memory.NewDemo()
	service := application.New(store, application.Options{AllowDemoAccount: true})
	h := newHarness(t, store, application.Options{AllowDemoAccount: true}, testSessions)
	token := loginHTTP(t, h.handler)
	p, err := service.RepeatPreviewV2(context.Background(), domain.DemoUserID, "ord-2026-002")
	if err != nil {
		t.Fatal(err)
	}
	input := contract.RepeatInput(p.Items[0], 2)
	encoded, _ := json.Marshal(input)
	valid := string(encoded)
	before := snapshot(t, store)
	for name, tc := range map[string]struct {
		body string
		keys []string
	}{
		"missing body": {"", []string{input.IdempotencyKey}}, "null": {"null", []string{input.IdempotencyKey}}, "missing items": {"{}", []string{input.IdempotencyKey}}, "null items": {`{"items":null}`, []string{input.IdempotencyKey}}, "empty items": {`{"items":[]}`, []string{input.IdempotencyKey}},
		"missing key": {valid, nil}, "empty key": {valid, []string{""}}, "short key": {valid, []string{"short"}}, "repeated key": {valid, []string{input.IdempotencyKey, input.IdempotencyKey}}, "comma key": {valid, []string{input.IdempotencyKey + "," + input.IdempotencyKey}}, "long key": {valid, []string{strings.Repeat("x", 129)}},
		"selected forbidden": {strings.Replace(valid, `"quantity":2`, `"quantity":2,"selected":true`, 1), []string{input.IdempotencyKey}}, "digest forbidden": {strings.Replace(valid, `{"items":`, `{"requestDigest":"abc","items":`, 1), []string{input.IdempotencyKey}},
	} {
		t.Run(name, func(t *testing.T) {
			w := repeatRequest(h, token, tc.body, tc.keys...)
			expectStatus(t, w, 422)
			if errorCode(t, w) != "validation_error" || !reflect.DeepEqual(before, snapshot(t, store)) {
				t.Fatal(w.Body.String())
			}
		})
	}
	first := repeatRequest(h, token, valid, input.IdempotencyKey)
	expectStatus(t, first, 200)
	if first.Header().Get("Idempotent-Replayed") != "" || first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID {
		t.Fatal(first.Header())
	}
	// First HTTP result can be lost: the same key/body explicitly recovers it.
	replay := repeatRequest(h, token, valid, input.IdempotencyKey)
	expectStatus(t, replay, 200)
	if replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal(replay.Header())
	}
	changed := repeatRequest(h, token, strings.Replace(valid, `"quantity":2`, `"quantity":1`, 1), input.IdempotencyKey)
	expectStatus(t, changed, 409)
	if errorCode(t, changed) != "idempotency_key_reused" {
		t.Fatal(changed.Body.String())
	}
	if strings.Contains(h.logs.String(), "requestDigest") || strings.Contains(h.logs.String(), input.IdempotencyKey) {
		t.Fatal("idempotency metadata logged")
	}
}

func (r dataCalls) CommitRepeatV2(ctx context.Context, user, order string, input domain.RepeatV2Input) (domain.RepeatV2Result, error) {
	r.calls.Add(1)
	return r.Store.CommitRepeatV2(ctx, user, order, input)
}

func TestRepeatV2ExpectedUserAndCookieCSRF(t *testing.T) {
	store := memory.NewDemo()
	calls := &atomic.Int64{}
	h := newHarness(t, dataCalls{Store: store, calls: calls}, application.Options{AllowDemoAccount: true}, testSessions)
	cookie, csrf := h.cookieLogin(nil)
	bearer := loginHTTP(t, h.handler)
	before := snapshot(t, store)
	for _, action := range []string{"preview", "commit"} {
		for _, headers := range []map[string]string{h.mutation(cookie, csrf), {"Authorization": "Bearer " + bearer}} {
			w := send(h, "POST", repeatPath+action, `{}`, headers, "usr-a")
			expectStatus(t, w, 409)
			if errorCode(t, w) != "authenticated_user_changed" || w.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID || w.Header().Get("Cache-Control") != "no-store" || calls.Load() != 0 {
				t.Fatal("precondition did not stop handler", w.Body.String())
			}
		}
		w := h.do("POST", repeatPath+action, `{}`, h.mutation(cookie, "wrong"))
		expectStatus(t, w, 403)
		if errorCode(t, w) != "csrf_failed" {
			t.Fatal(w.Body.String())
		}
		w = h.do("POST", repeatPath+action, `{}`, nil)
		expectStatus(t, w, 401)
	}
	if !reflect.DeepEqual(before, snapshot(t, store)) {
		t.Fatal("rejected request changed state")
	}
	w := send(h, "POST", repeatPath+"preview", "", h.mutation(cookie, csrf), domain.DemoUserID)
	expectStatus(t, w, 200)
}
