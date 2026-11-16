package historiaquery

import "time"

// Warsaw helpers shared by both siblings' historia schedulers: snapshot rows are keyed by the
// Europe/Warsaw calendar date, never by UTC (the container runs UTC unless TZ is set).

func warsawLocation() *time.Location { return WarsawLocation() }

// WarsawLocation returns Europe/Warsaw, falling back to UTC when the zoneinfo
// database is unavailable (slim containers).
func WarsawLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return time.UTC
	}
	return loc
}

// NowInWarsaw returns the current instant in Europe/Warsaw.
func NowInWarsaw() time.Time { return time.Now().In(warsawLocation()) }

// WarsawDate returns the calendar date (UTC midnight) of now in Europe/Warsaw.
func WarsawDate(now time.Time) time.Time { return dateOnly(now.In(warsawLocation())) }

// NextDailyFire returns the next 23:00 (Europe/Warsaw wall time) strictly after
// now — the Go spelling of cron "0 0 23 * * *" with zone Europe/Warsaw, shared by
// both siblings' HistoriaSchedulers.
func NextDailyFire(now time.Time) time.Time {
	fire := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, now.Location())
	if !fire.After(now) {
		fire = fire.AddDate(0, 0, 1)
	}
	return fire
}
