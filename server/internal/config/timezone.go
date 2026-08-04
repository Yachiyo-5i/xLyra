package config

import (
	"os"
	"strings"
	"time"
)

const DefaultTimeZone = "Asia/Shanghai"

type TimeZone struct {
	Name     string
	Location *time.Location
}

func ResolveTimeZone() TimeZone {
	name := strings.TrimSpace(os.Getenv("TZ"))
	if name == "" {
		name = strings.TrimSpace(os.Getenv("TimeZone"))
	}
	return LoadTimeZone(name)
}

func TimeZoneOrDefault(timeZones ...TimeZone) TimeZone {
	if len(timeZones) > 0 && timeZones[0].Location != nil {
		return timeZones[0]
	}
	return ResolveTimeZone()
}

func LoadTimeZone(name string) TimeZone {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultTimeZone
	}
	location, err := time.LoadLocation(name)
	if err == nil {
		return TimeZone{Name: name, Location: location}
	}
	location, err = time.LoadLocation(DefaultTimeZone)
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	return TimeZone{Name: DefaultTimeZone, Location: location}
}

func (tz TimeZone) In(value time.Time) time.Time {
	return value.In(tz.location())
}

func (tz TimeZone) Format(value time.Time, layout string) string {
	return tz.In(value).Format(layout)
}

func (tz TimeZone) StartOfDay(value time.Time) time.Time {
	local := tz.In(value)
	year, month, day := local.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, tz.location())
}

func (tz TimeZone) StartOfWeek(value time.Time) time.Time {
	start := tz.StartOfDay(value)
	daysSinceMonday := (int(start.Weekday()) + 6) % 7
	return start.AddDate(0, 0, -daysSinceMonday)
}

func (tz TimeZone) StartOfHour(value time.Time) time.Time {
	local := tz.In(value)
	year, month, day := local.Date()
	return time.Date(year, month, day, local.Hour(), 0, 0, 0, tz.location())
}

func (tz TimeZone) location() *time.Location {
	if tz.Location != nil {
		return tz.Location
	}
	return LoadTimeZone(tz.Name).Location
}
