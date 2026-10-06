// Package schedule works out when a daily or weekly job is due.
package schedule

import "time"

// A run that is more than MaxDelay late is skipped, for example when the
// manager was off. The next slot runs as normal.
const MaxDelay = time.Hour

// Slot is a time of day that repeats every day, or every week on Weekday.
type Slot struct {
	Weekly  bool
	Weekday time.Weekday
	Hour    int
	Minute  int
}

func Daily(hour, minute int) Slot {
	return Slot{Hour: hour, Minute: minute}
}

func Weekly(weekday time.Weekday, hour, minute int) Slot {
	return Slot{Weekly: true, Weekday: weekday, Hour: hour, Minute: minute}
}

// Last is the most recent slot at or before now.
func (s Slot) Last(now time.Time) time.Time {
	slot := time.Date(now.Year(), now.Month(), now.Day(), s.Hour, s.Minute, 0, 0, now.Location())
	if s.Weekly {
		slot = slot.AddDate(0, 0, int(s.Weekday)-int(now.Weekday()))
	}
	if slot.After(now) {
		slot = slot.AddDate(0, 0, -s.days())
	}

	return slot
}

// Next is the first slot after now.
func (s Slot) Next(now time.Time) time.Time {
	return s.Last(now).AddDate(0, 0, s.days())
}

// Due returns the slot to run now, if there is one.
func (s Slot) Due(now, lastRun time.Time) (time.Time, bool) {
	slot := s.Last(now)
	if !slot.After(lastRun) || now.Sub(slot) > MaxDelay {
		return time.Time{}, false
	}

	return slot, true
}

func (s Slot) days() int {
	if s.Weekly {
		return 7
	}

	return 1
}
