package domain

import (
	"testing"
	"time"
)

func TestCollectionVisibleAtMatchesInclusiveSchedulePredicate(t *testing.T) {
	now := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		collection Collection
		visible    bool
	}{
		{name: "open ended", collection: Collection{}, visible: true},
		{name: "already started", collection: Collection{StartsAt: now.Add(-time.Hour)}, visible: true},
		{name: "future", collection: Collection{StartsAt: now.Add(time.Hour)}, visible: false},
		{name: "expired", collection: Collection{EndsAt: now.Add(-time.Hour)}, visible: false},
		{name: "start boundary is inclusive", collection: Collection{StartsAt: now}, visible: true},
		{name: "end boundary is inclusive", collection: Collection{EndsAt: now}, visible: true},
		{name: "bounded active", collection: Collection{StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)}, visible: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CollectionVisibleAt(test.collection, now); got != test.visible {
				t.Fatalf("visible=%t want=%t", got, test.visible)
			}
		})
	}
}
