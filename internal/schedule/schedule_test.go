package schedule

import (
	"testing"
	"time"
)

func TestWeekly(t *testing.T) {
	slot := Weekly(time.Sunday, 4, 0)
	sunday := time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		due     bool
	}{
		{"at the slot", sunday, time.Time{}, true},
		{"half an hour late", sunday.Add(30 * time.Minute), time.Time{}, true},
		{"already ran", sunday.Add(time.Minute), sunday, false},
		{"too late", sunday.Add(2 * time.Hour), time.Time{}, false},
		{"before the slot", sunday.Add(-time.Minute), sunday.AddDate(0, 0, -7), false},
	}
	for _, c := range cases {
		if _, due := slot.Due(c.now, c.lastRun); due != c.due {
			t.Errorf("%s: expected due=%v", c.name, c.due)
		}
	}

	if next := slot.Next(sunday.Add(-time.Hour)); !next.Equal(sunday) {
		t.Errorf("next = %v, want %v", next, sunday)
	}
	if next := slot.Next(sunday); !next.Equal(sunday.AddDate(0, 0, 7)) {
		t.Errorf("next at the slot = %v", next)
	}
}

func TestDaily(t *testing.T) {
	slot := Daily(5, 30)
	today := time.Date(2026, 10, 2, 5, 30, 0, 0, time.UTC)

	if last := slot.Last(today.Add(-time.Minute)); !last.Equal(today.AddDate(0, 0, -1)) {
		t.Errorf("last before the slot = %v", last)
	}
	if got, due := slot.Due(today.Add(10*time.Minute), today.AddDate(0, 0, -1)); !due || !got.Equal(today) {
		t.Errorf("due = %v %v", got, due)
	}
	if _, due := slot.Due(today.Add(time.Minute), today); due {
		t.Error("due twice on the same day")
	}
	if next := slot.Next(today.Add(time.Hour)); !next.Equal(today.AddDate(0, 0, 1)) {
		t.Errorf("next = %v", next)
	}
}

func TestDailyKeepsTheTimeOfDayOverDST(t *testing.T) {
	brussels, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Skip("no time zone data")
	}
	// Summer time ends on 25 October 2026.
	before := time.Date(2026, 10, 24, 6, 0, 0, 0, brussels)

	if next := Daily(5, 0).Next(before); next.Hour() != 5 || next.Day() != 25 {
		t.Errorf("next = %v", next)
	}
}
