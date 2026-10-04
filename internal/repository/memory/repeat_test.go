package memory

import (
	"sweetshop/internal/domain"
	"sweetshop/tests/contract"
	"testing"
)

func TestRepeatV2Contract(t *testing.T) {
	contract.RunRepeatV2(t, func(t *testing.T) contract.RepeatFixture {
		s := NewDemo()
		return contract.RepeatFixture{Repo: s,
			ChangeProduct: func(change func(*domain.Product)) {
				s.mu.Lock()
				defer s.mu.Unlock()
				p := s.products["p01"]
				change(&p)
				s.products[p.ID] = p
			},
			DeleteProduct: func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				delete(s.products, "p01")
				for _, cart := range s.carts {
					delete(cart, "p01")
				}
			},
			DuplicateSource: func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				line := s.orders["usr-demo"][0].Items[0]
				line.SourcePosition = 9
				s.orders["usr-demo"][0].Items = append(s.orders["usr-demo"][0].Items, line)
			},
			ReceiptCount: func() int { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.repeatKeys["usr-demo"]) },
		}
	})
}
