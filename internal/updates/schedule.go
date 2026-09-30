package updates

import "time"

// A run that is more than this late is skipped, for example when the manager
// was off. The next week runs as normal.
const maxScheduleDelay = time.Hour

// lastSlot is the most recent scheduled moment at or before now.
func lastSlot(schedule Schedule, now time.Time) time.Time {
	slot := time.Date(now.Year(), now.Month(), now.Day(), schedule.Hour, schedule.Minute, 0, 0, now.Location())
	slot = slot.AddDate(0, 0, schedule.Weekday-int(now.Weekday()))
	if slot.After(now) {
		slot = slot.AddDate(0, 0, -7)
	}

	return slot
}

// nextSlot is the next scheduled moment after now.
func nextSlot(schedule Schedule, now time.Time) time.Time {
	return lastSlot(schedule, now).AddDate(0, 0, 7)
}

// dueSlot returns the slot to run now, if there is one.
func dueSlot(schedule Schedule, now, lastRun time.Time) (time.Time, bool) {
	if !schedule.Enabled {
		return time.Time{}, false
	}

	slot := lastSlot(schedule, now)
	if !slot.After(lastRun) || now.Sub(slot) > maxScheduleDelay {
		return time.Time{}, false
	}

	return slot, true
}
