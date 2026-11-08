package main

import (
	"strings"
	"testing"
)

// Every embedded article set loads and passes the same checks the seed applies before writing.
func TestKBSetsLoad(t *testing.T) {
	entries, err := kbFiles.ReadDir("kb")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no KB sets embedded")
	}
	for _, e := range entries {
		articles, err := loadKBSet(e.Name())
		if err != nil {
			t.Fatalf("set %s: %v", e.Name(), err)
		}
		seen := map[string]bool{}
		for _, a := range articles {
			if seen[a.Slug] {
				t.Fatalf("set %s: duplicate slug %q", e.Name(), a.Slug)
			}
			seen[a.Slug] = true
		}
	}
}

func TestHadiaSafetyArticle(t *testing.T) {
	articles, err := loadKBSet("hadia")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range articles {
		if a.Slug == "recognise-real-hadia-messages" {
			if !strings.Contains(a.Content, "hadiafrica.com") {
				t.Fatal("the safety article must name the real address")
			}
			return
		}
	}
	t.Fatal("recognise-real-hadia-messages is missing from the hadia set")
}

func TestKBSetNameIsChecked(t *testing.T) {
	for _, bad := range []string{"", "../kb", "hadia/x", "a.b", "nope"} {
		if _, err := loadKBSet(bad); err == nil {
			t.Fatalf("KB_SET %q was accepted", bad)
		}
	}
}
