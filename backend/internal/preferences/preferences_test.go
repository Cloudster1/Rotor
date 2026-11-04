package preferences

import (
	"errors"
	"strings"
	"testing"
)

func TestATimeZoneIsOneTheIANADatabaseNamesOrNone(t *testing.T) {
	for _, zone := range []string{"", "UTC", "Europe/Berlin", "America/Argentina/Buenos_Aires"} {
		if err := (Preferences{TimeZone: zone}).Check(); err != nil {
			t.Errorf("%q = %v, want accepted", zone, err)
		}
	}
	for _, zone := range []string{"Local", "Mars/Olympus", " Europe/Berlin", "Europe/Berlin ", strings.Repeat("A", 65), "../etc/passwd"} {
		var invalid *Invalid
		if err := (Preferences{TimeZone: zone}).Check(); !errors.As(err, &invalid) || invalid.Fields["timeZone"] == "" {
			t.Errorf("%q = %v, want refused with a sentence for timeZone", zone, err)
		}
	}
}
