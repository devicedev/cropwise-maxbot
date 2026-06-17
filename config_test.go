package main

import (
	"testing"
	"time"
)

func TestParseFlexibleTimeWithoutZoneUsesLocalLocation(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() {
		time.Local = oldLocal
	})

	got, err := parseFlexibleTime("2026-06-10T00:00:00")
	if err != nil {
		t.Fatalf("parseFlexibleTime returned error: %v", err)
	}

	want := "2026-06-10T00:00:00+03:00"
	if got.Format(time.RFC3339) != want {
		t.Fatalf("unexpected parsed time: want %q, got %q", want, got.Format(time.RFC3339))
	}
}

func TestCropwiseTimeUsesLocalOffset(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() {
		time.Local = oldLocal
	})

	got := cropwiseTime(time.Date(2026, 6, 16, 12, 39, 14, 0, time.UTC))
	want := "2026-06-16T15:39:14+03:00"
	if got != want {
		t.Fatalf("unexpected cropwise time: want %q, got %q", want, got)
	}
}
