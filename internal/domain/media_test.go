package domain

import (
	"reflect"
	"testing"
)

func TestResolveProductMediaCoexistence(t *testing.T) {
	a := ProductMediaItem{URL: "/media/a"}
	b := ProductMediaItem{URL: "/media/b"}
	for _, tc := range []struct {
		name    string
		managed *ManagedMedia
		image   string
		gallery []string
	}{
		{"legacy", nil, "/assets/a", []string{"/assets/b", "/assets/a"}},
		{"detached", &ManagedMedia{Gallery: []ProductMediaItem{}}, "/assets/a", []string{"/assets/b", "/assets/a"}},
		{"primary only", &ManagedMedia{Primary: &a}, a.URL, []string{}},
		{"gallery only", &ManagedMedia{Gallery: []ProductMediaItem{b}}, "/assets/a", []string{b.URL}},
		{"managed", &ManagedMedia{Primary: &a, Gallery: []ProductMediaItem{b}}, a.URL, []string{b.URL}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image, gallery := ResolveProductMedia("/assets/a", []string{"/assets/b", "/assets/a"}, tc.managed, false)
			if image != tc.image || !reflect.DeepEqual(gallery, tc.gallery) {
				t.Fatalf("%s %+v", image, gallery)
			}
		})
	}
}

func TestResolveProductMediaRetiredLegacy(t *testing.T) {
	a := ProductMediaItem{URL: "/media/a"}
	b := ProductMediaItem{URL: "/media/b"}
	for _, tc := range []struct {
		name    string
		managed *ManagedMedia
		image   string
		gallery []string
	}{
		{"no references", nil, "", []string{}},
		{"detached", &ManagedMedia{Gallery: []ProductMediaItem{}}, "", []string{}},
		{"primary only", &ManagedMedia{Primary: &a}, a.URL, []string{}},
		{"gallery only", &ManagedMedia{Gallery: []ProductMediaItem{b}}, "", []string{b.URL}},
		{"managed", &ManagedMedia{Primary: &a, Gallery: []ProductMediaItem{b}}, a.URL, []string{b.URL}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image, gallery := ResolveProductMedia("/assets/a", []string{"/assets/b"}, tc.managed, true)
			if image != tc.image || !reflect.DeepEqual(gallery, tc.gallery) {
				t.Fatalf("%s %+v", image, gallery)
			}
		})
	}
}
