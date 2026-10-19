package triggers

import (
	"testing"
	"time"
)

func TestASchedulesFieldsAndZoneAreChecked(t *testing.T) {
	for _, c := range []struct{ cron, zone, field string }{
		{"TZ=Europe/Berlin 0 8 * * 1", "UTC", FieldCron},
		{"CRON_TZ=UTC 0 8 * * 1", "UTC", FieldCron},
		{"0 0 8 * * 1", "UTC", FieldCron},
		{"@daily", "UTC", FieldCron},
		{"61 8 * * 1", "UTC", FieldCron},
		{"* * * * *", "UTC", FieldCron},
		{"*/2 8 * * *", "UTC", FieldCron},
		{"0 8 * * 1", "", FieldTimeZone},
		{"0 8 * * 1", "Local", FieldTimeZone},
		{"0 8 * * 1", "Mars/Olympus", FieldTimeZone},
	} {
		_, err := ParseSchedule(c.cron, c.zone)
		p, ok := err.(*ScheduleProblem)
		if !ok || p.Field != c.field {
			t.Errorf("%q in %q = %v, want a problem with %s", c.cron, c.zone, err, c.field)
		}
	}
	for _, ok := range []string{"0 8 * * 1", "*/5 * * * *", "0,30 9-17 * * 1-5", "0 0 1 1 *"} {
		if _, err := ParseSchedule(ok, "Europe/Berlin"); err != nil {
			t.Errorf("%q = %v", ok, err)
		}
	}
	// Minute 58 then minute 1 of the next hour is three minutes apart.
	if _, err := ParseSchedule("1,58 * * * *", "UTC"); err == nil {
		t.Error("firings three minutes apart across the hour were allowed")
	}
}

func TestASchedulesNextRunFollowsItsZoneAcrossSummerTime(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	monday, _ := ParseSchedule("0 8 * * 1", "Europe/Berlin")
	if got := monday.Next(time.Date(2026, 10, 19, 9, 0, 0, 0, berlin)); !got.Equal(time.Date(2026, 10, 26, 8, 0, 0, 0, berlin)) {
		t.Errorf("the Monday after = %v", got.In(berlin))
	}

	// 2:30 does not exist on 29 March 2026 in Berlin: that day is skipped.
	early, _ := ParseSchedule("30 2 * * *", "Europe/Berlin")
	got := early.NextRuns(time.Date(2026, 3, 28, 12, 0, 0, 0, berlin), 2)
	want := []time.Time{time.Date(2026, 3, 30, 2, 30, 0, 0, berlin)}
	if len(got) < 1 || !got[0].Equal(want[0]) {
		t.Errorf("across the spring change = %v, want %v first", inZone(got, berlin), want)
	}

	// 2:30 happens twice on 25 October 2026 in Berlin: it fires once.
	got = early.NextRuns(time.Date(2026, 10, 24, 12, 0, 0, 0, berlin), 3)
	if len(got) != 3 || !got[0].Equal(time.Date(2026, 10, 25, 2, 30, 0, 0, time.FixedZone("CEST", 2*3600))) ||
		!got[1].Equal(time.Date(2026, 10, 26, 2, 30, 0, 0, berlin)) {
		t.Errorf("across the autumn change = %v", inZone(got, berlin))
	}

	// Every half hour through the repeated hour fires each wall time once.
	half, _ := ParseSchedule("0,30 * * * *", "Europe/Berlin")
	got = half.NextRuns(time.Date(2026, 10, 25, 1, 45, 0, 0, berlin), 4)
	seen := map[string]bool{}
	for _, t1 := range got {
		key := t1.In(berlin).Format("15:04")
		if seen[key] {
			t.Errorf("%s fired twice: %v", key, inZone(got, berlin))
		}
		seen[key] = true
	}
}

func inZone(ts []time.Time, loc *time.Location) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.In(loc).Format(time.RFC3339))
	}
	return out
}
