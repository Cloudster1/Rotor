package triggers

import (
	"fmt"
	"strings"
	"time"
	// The zones a schedule names, for test containers whose image carries none; the release image has tzdata too.
	_ "time/tzdata"

	"github.com/robfig/cron/v3"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Fields of a schedule that a problem names.
const (
	FieldCron     = "cron"
	FieldTimeZone = "timeZone"
)

// ScheduleProblem is a schedule Rotor refuses, with the field it is about and a sentence saying what to do.
type ScheduleProblem struct {
	Field   string
	Message string
}

func (p *ScheduleProblem) Error() string { return p.Message }

// fiveFields reads cron as crontab(5) does: minute, hour, day of month, month, day of week; no seconds, no @daily.
var fiveFields = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// starBit is how the cron library marks a field written as *, which matters for the day fields.
const starBit = 1 << 63

// maxSkips bounds how many candidate times Next steps over across a change of summer time.
const maxSkips = 8

// The clock and the cron format, named so the arithmetic below reads.
const (
	cronFields     = 5
	minutesPerHour = 60
	hoursPerDay    = 24
	lastHour       = hoursPerDay - 1
	// offsetLookback reaches before any change of a zone's offset that could make a wall time happen twice.
	offsetLookback = 12 * time.Hour
)

// Schedule is a checked schedule: five cron fields read in a time zone.
type Schedule struct {
	spec *cron.SpecSchedule
	loc  *time.Location
}

// ParseSchedule checks a schedule: five fields, an IANA time zone, and firings at least config.MinScheduleInterval apart.
func ParseSchedule(expr, zone string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	upper := strings.ToUpper(expr)
	if strings.HasPrefix(upper, "TZ=") || strings.HasPrefix(upper, "CRON_TZ=") {
		return Schedule{}, &ScheduleProblem{FieldCron, "Leave the time zone out of the cron fields and choose it in its own field."}
	}
	if len(strings.Fields(expr)) != cronFields {
		return Schedule{}, &ScheduleProblem{FieldCron, "Write five fields: minute, hour, day of month, month and day of week, such as 0 8 * * 1."}
	}
	if zone == "" || zone == "Local" {
		return Schedule{}, &ScheduleProblem{FieldTimeZone, "Choose a time zone by its name, such as Europe/Berlin or UTC."}
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Schedule{}, &ScheduleProblem{FieldTimeZone, fmt.Sprintf("%s is not a time zone Rotor knows; choose one such as Europe/Berlin or UTC.", zone)}
	}
	parsed, err := fiveFields.Parse(expr)
	if err != nil {
		return Schedule{}, &ScheduleProblem{FieldCron, "These cron fields do not read: " + strings.TrimPrefix(err.Error(), "failed to parse ") + "."}
	}
	spec := parsed.(*cron.SpecSchedule)
	spec.Location = loc
	if gap := shortestGap(spec); gap < config.MinScheduleInterval {
		return Schedule{}, &ScheduleProblem{FieldCron, fmt.Sprintf("A schedule may fire every %d minutes at most; these fields fire %d minutes apart.",
			int(config.MinScheduleInterval.Minutes()), int(gap.Minutes()))}
	}
	return Schedule{spec: spec, loc: loc}, nil
}

// shortestGap is the shortest time between two firings the minute and hour fields allow, whatever the day.
func shortestGap(s *cron.SpecSchedule) time.Duration {
	minutes, hours := set(s.Minute, minutesPerHour), set(s.Hour, hoursPerDay)
	gap := time.Hour
	for i := 1; i < len(minutes); i++ {
		gap = min(gap, time.Duration(minutes[i]-minutes[i-1])*time.Minute)
	}
	// Across the hour, when two hours in a row are allowed, the last minute of one meets the first of the next.
	for i, h := range hours {
		if len(minutes) > 0 && (i+1 < len(hours) && hours[i+1] == h+1 || h == lastHour && hours[0] == 0) {
			gap = min(gap, time.Duration(minutesPerHour-minutes[len(minutes)-1]+minutes[0])*time.Minute)
		}
	}
	return gap
}

func set(field uint64, n int) []int {
	var out []int
	for v := range n {
		if field&(1<<uint(v)) != 0 {
			out = append(out, v)
		}
	}
	return out
}

// Next is the first firing after t: a time the fields name that exists on its day, never the same wall time twice.
func (s Schedule) Next(after time.Time) time.Time {
	t := after
	for range maxSkips {
		t = s.spec.Next(t)
		if t.IsZero() {
			return t
		}
		if local := t.In(s.loc); s.matches(local) && !s.repeated(local) {
			return t
		}
	}
	return time.Time{}
}

// NextRuns are the next n firings after t, in UTC.
func (s Schedule) NextRuns(after time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	for t := after; len(out) < n; {
		t = s.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t.UTC())
	}
	return out
}

// matches says whether a wall time is one the fields name; across a change to summer time the library may land on
// a later time that they do not, which is skipped.
func (s Schedule) matches(t time.Time) bool {
	if s.spec.Minute&(1<<uint(t.Minute())) == 0 || s.spec.Hour&(1<<uint(t.Hour())) == 0 || s.spec.Month&(1<<uint(t.Month())) == 0 {
		return false
	}
	dom, dow := s.spec.Dom&(1<<uint(t.Day())) != 0, s.spec.Dow&(1<<uint(t.Weekday())) != 0
	if s.spec.Dom&starBit != 0 || s.spec.Dow&starBit != 0 {
		return dom && dow
	}
	return dom || dow
}

// repeated says whether a wall time is the second of two: when the clocks go back, a time happens twice and fires
// the first time only.
func (s Schedule) repeated(t time.Time) bool {
	_, now := t.Zone()
	_, before := t.Add(-offsetLookback).In(s.loc).Zone()
	if before <= now {
		return false
	}
	return sameWallMinute(t.Add(-time.Duration(before-now)*time.Second).In(s.loc), t)
}

func sameWallMinute(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay() && a.Hour() == b.Hour() && a.Minute() == b.Minute()
}
