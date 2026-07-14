package calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ResolveLocalDateTime turns user-facing wall-time fields into an instant in
// the selected IANA timezone. DST gaps are rejected, and repeated times require
// an explicit first/second occurrence so callers never silently guess.
func ResolveLocalDateTime(date, clock, timezone, occurrence string) (time.Time, error) {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q", timezone)
	}
	fields, err := time.Parse("2006-01-02 15:04", strings.TrimSpace(date)+" "+strings.TrimSpace(clock))
	if err != nil {
		return time.Time{}, fmt.Errorf("local date and time must use YYYY-MM-DD and HH:MM: %w", err)
	}
	candidates := wallTimeCandidates(fields.Year(), fields.Month(), fields.Day(), fields.Hour(), fields.Minute(), 0, 0, loc)
	switch len(candidates) {
	case 0:
		return time.Time{}, fmt.Errorf("%s %s does not exist in %s because the clock moves forward; choose another local time", date, clock, timezone)
	case 1:
		if strings.TrimSpace(occurrence) != "" {
			return time.Time{}, fmt.Errorf("-occurrence is only valid when the local time occurs twice")
		}
		return candidates[0], nil
	default:
		switch strings.ToLower(strings.TrimSpace(occurrence)) {
		case "first":
			return candidates[0], nil
		case "second":
			return candidates[1], nil
		default:
			return time.Time{}, fmt.Errorf("%s %s occurs twice in %s because the clock moves back; choose -occurrence first or -occurrence second", date, clock, timezone)
		}
	}
}

// NextOccurrence advances using the named timezone's calendar, preserving the
// local wall clock through offset changes. A nonexistent wall time is skipped;
// when a wall time occurs twice, the series keeps its previous UTC offset when
// possible and otherwise chooses the earlier instant deterministically.
func NextOccurrence(at time.Time, recurrence Recurrence, timezone string) (time.Time, bool) {
	if recurrence == "" || recurrence == RecurrenceNone {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := at.In(loc)
	days := 1
	if recurrence == RecurrenceWeekly {
		days = 7
	}
	_, previousOffset := local.Zone()
	for attempts := 0; attempts < 370; attempts++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+days, 12, 0, 0, 0, loc)
		if recurrence == RecurrenceWeekdays && (date.Weekday() == time.Saturday || date.Weekday() == time.Sunday) {
			days++
			continue
		}
		candidates := wallTimeCandidates(date.Year(), date.Month(), date.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), loc)
		if len(candidates) > 0 {
			for _, candidate := range candidates {
				_, offset := candidate.Zone()
				if offset == previousOffset {
					return candidate, true
				}
			}
			return candidates[0], true
		}
		// The requested local clock is inside a forward DST gap. Skip that
		// calendar occurrence instead of silently shifting its wall time.
		if recurrence == RecurrenceWeekly {
			days += 7
		} else {
			days++
		}
	}
	return time.Time{}, false
}

func wallTimeCandidates(year int, month time.Month, day, hour, minute, second, nanosecond int, loc *time.Location) []time.Time {
	wallUTC := time.Date(year, month, day, hour, minute, second, nanosecond, time.UTC)
	offsets := map[int]struct{}{}
	for sampleHour := -36; sampleHour <= 36; sampleHour += 6 {
		sample := wallUTC.Add(time.Duration(sampleHour) * time.Hour)
		_, offset := sample.In(loc).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := wallUTC.Add(-time.Duration(offset) * time.Second).In(loc)
		if candidate.Year() == year && candidate.Month() == month && candidate.Day() == day && candidate.Hour() == hour && candidate.Minute() == minute && candidate.Second() == second && candidate.Nanosecond() == nanosecond {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	return candidates
}

func advanceItem(item *Item) bool {
	previous := item.TriggerAt()
	next, ok := NextOccurrence(previous, item.Recurrence, item.Timezone)
	if !ok {
		return false
	}
	switch item.Kind {
	case KindEvent:
		start, end, eventOK := nextEventOccurrence(*item.StartAt, *item.EndAt, next, item.Recurrence, item.Timezone)
		if !eventOK {
			return false
		}
		next = start
		item.StartAt, item.EndAt = &start, &end
	case KindReminder:
		item.NotifyAt = &next
	default:
		item.DueAt = &next
	}
	item.NextAt = next
	item.Status = StatusScheduled
	item.FailureReason = ""
	return true
}

func nextEventOccurrence(previousStart, previousEnd, firstStart time.Time, recurrence Recurrence, timezone string) (time.Time, time.Time, bool) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	startLocal, endLocal := previousStart.In(loc), previousEnd.In(loc)
	startOrdinal := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
	endOrdinal := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
	endDayOffset := int(endOrdinal - startOrdinal)
	_, previousEndOffset := endLocal.Zone()
	start := firstStart
	for attempts := 0; attempts < 370; attempts++ {
		localStart := start.In(loc)
		endDate := time.Date(localStart.Year(), localStart.Month(), localStart.Day()+endDayOffset, 12, 0, 0, 0, loc)
		endCandidates := wallTimeCandidates(endDate.Year(), endDate.Month(), endDate.Day(), endLocal.Hour(), endLocal.Minute(), endLocal.Second(), endLocal.Nanosecond(), loc)
		if end, ok := chooseWallCandidate(endCandidates, previousEndOffset); ok && end.After(start) {
			return start, end, true
		}
		// If either event boundary is inside a DST gap, skip the whole event
		// occurrence instead of creating a shifted or day-long interval.
		nextStart, nextOK := NextOccurrence(start, recurrence, timezone)
		if !nextOK {
			return time.Time{}, time.Time{}, false
		}
		start = nextStart
	}
	return time.Time{}, time.Time{}, false
}

func chooseWallCandidate(candidates []time.Time, preferredOffset int) (time.Time, bool) {
	for _, candidate := range candidates {
		_, offset := candidate.Zone()
		if offset == preferredOffset {
			return candidate, true
		}
	}
	if len(candidates) == 0 {
		return time.Time{}, false
	}
	return candidates[0], true
}
