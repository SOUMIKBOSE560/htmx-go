package models

import (
	"strings"
	"testing"
	"time"
)

func TestValidStatus(t *testing.T) {
	for _, s := range []string{StatusToRead, StatusReading, StatusFinished} {
		if !ValidStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "read", "DONE", "finished "} {
		if ValidStatus(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestStatsProgress(t *testing.T) {
	cases := []struct {
		goal, finished int
		want           int
	}{
		{10, 0, 0},
		{10, 5, 50},
		{10, 10, 100},
		{10, 15, 100}, // over-achievement caps at 100
		{0, 5, 0},     // no goal → no progress
	}
	for _, c := range cases {
		s := Stats{Goal: c.goal, FinishedYear: c.finished}
		if got := s.Progress(); got != c.want {
			t.Errorf("Progress(goal=%d, finished=%d) = %d, want %d", c.goal, c.finished, got, c.want)
		}
	}
}

func TestBookStars(t *testing.T) {
	r := func(n int) *int { return &n }

	cases := []struct {
		rating *int
		want   string
	}{
		{nil, ""},
		{r(5), "★★★★★"},
		{r(3), "★★★☆☆"},
		{r(1), "★☆☆☆☆"},
		{r(0), "★☆☆☆☆"}, // clamped up
		{r(9), "★★★★★"}, // clamped down
	}
	for _, c := range cases {
		b := Book{Rating: c.rating}
		if got := b.Stars(); got != c.want {
			t.Errorf("Stars(%v) = %q, want %q", c.rating, got, c.want)
		}
	}
}

func TestBookRatingValueAndText(t *testing.T) {
	r := 4
	b := Book{Rating: &r}
	if b.RatingValue() != 4 {
		t.Errorf("RatingValue() = %d, want 4", b.RatingValue())
	}
	if (Book{}).RatingValue() != 0 {
		t.Error("unrated book must report 0")
	}

	review := "loved it"
	b2 := Book{Review: &review}
	if b2.ReviewText() != "loved it" {
		t.Errorf("ReviewText() = %q", b2.ReviewText())
	}
	if (Book{}).ReviewText() != "" {
		t.Error("nil review must be empty string")
	}
}

func TestBookFinishedDate(t *testing.T) {
	if (Book{}).FinishedDate() != "" {
		t.Error("unfinished book must have no date")
	}
	when := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	b := Book{FinishedAt: &when}
	if !strings.Contains(b.FinishedDate(), "2026") {
		t.Errorf("FinishedDate() = %q, want a 2026 date", b.FinishedDate())
	}
}
