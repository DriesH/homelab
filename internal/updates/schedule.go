package updates

import (
	"time"

	"homelab/internal/schedule"
)

func (s Schedule) slot() schedule.Slot {
	return schedule.Weekly(time.Weekday(s.Weekday), s.Hour, s.Minute)
}

// nextSlot is the next scheduled moment after now.
func nextSlot(s Schedule, now time.Time) time.Time {
	return s.slot().Next(now)
}

// dueSlot returns the slot to run now, if there is one.
func dueSlot(s Schedule, now, lastRun time.Time) (time.Time, bool) {
	if !s.Enabled {
		return time.Time{}, false
	}

	return s.slot().Due(now, lastRun)
}
