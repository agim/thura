package calendar

import (
	"errors"
	"github.com/agim/lidza/pkg/router"
	"github.com/teambition/rrule-go"
	"strings"
	q "thura/db/queries/gen"
	"thura/schema"
	"time"
	_ "time/tzdata"
)

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func timing(in schema.EventInput) (time.Time, time.Time, error) {
	zone, err := time.LoadLocation(in.TimeZone)
	if in.TimeZone == "Local" {
		return time.Time{}, time.Time{}, router.Errorf(422, "use an explicit IANA timezone")
	}
	if err != nil {
		return time.Time{}, time.Time{}, router.Errorf(422, "use a valid IANA timezone")
	}
	var start, end time.Time
	if in.AllDay {
		if in.StartsAt != nil || in.EndsAt != nil {
			return start, end, router.Errorf(422, "all-day events use dates, not timestamps")
		}
		start, err = time.Parse("2006-01-02", text(in.StartDate))
		if err == nil {
			end, err = time.Parse("2006-01-02", text(in.EndDate))
		}
	} else {
		if text(in.StartDate) != "" || text(in.EndDate) != "" || in.StartsAt == nil || in.EndsAt == nil {
			return start, end, router.Errorf(422, "timed events need start and end timestamps")
		}
		start, end = in.StartsAt.In(zone), in.EndsAt.In(zone)
	}
	if err != nil || start.Year() < 2000 || start.Year() > 2100 || !end.After(start) || end.Sub(start) > 366*24*time.Hour {
		return start, end, router.Errorf(422, "event dates must be ordered, within 2000–2100, and at most a year long")
	}
	return start, end, nil
}

// Recurrence skips nonexistent wall-clock times before applying COUNT, as
// RFC 5545 requires. The underlying iterator is bounded by UNTIL even when
// a rule has no valid dates, so a malformed sparse rule cannot run forever.
type Recurrence struct {
	rule                 *rrule.RRule
	count                int
	hour, minute, second int
	allDay               bool
}

func (r *Recurrence) Iterator() func() (time.Time, bool) {
	next := r.rule.Iterator()
	produced := 0
	return func() (time.Time, bool) {
		if r.count > 0 && produced >= r.count {
			return time.Time{}, false
		}
		for {
			t, ok := next()
			if !ok {
				return t, false
			}
			if !r.allDay && (t.Hour() != r.hour || t.Minute() != r.minute || t.Second() != r.second) {
				continue
			}
			produced++
			return t, true
		}
	}
}
func Rule(raw string, start time.Time, allDay bool) (*Recurrence, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return nil, nil
	}
	if strings.ContainsAny(raw, "\r\n") {
		return nil, router.Errorf(422, "RRULE must be a single recurrence rule")
	}
	options, err := rrule.StrToROptionInLocation(raw, start.Location())
	if err != nil {
		return nil, router.Errorf(422, "invalid recurrence rule")
	}
	if len(options.Byeaster) > 0 || options.Freq > rrule.DAILY || options.Interval > 366 || len(options.Byhour) > 1 || len(options.Byminute) > 1 || len(options.Bysecond) > 1 || len(options.Bysetpos) > 31 || len(options.Byweekday) > 14 || len(options.Bymonthday) > 62 || len(options.Byyearday) > 366 || len(options.Byweekno) > 53 {
		return nil, router.Errorf(422, "recurrence is outside the supported bounds")
	}
	if allDay && (len(options.Byhour) > 0 || len(options.Byminute) > 0 || len(options.Bysecond) > 0) {
		return nil, router.Errorf(422, "all-day recurrence cannot contain clock times")
	}
	if options.Count > 0 && !options.Until.IsZero() {
		return nil, router.Errorf(422, "use COUNT or UNTIL, not both")
	}
	if options.Count < 0 || options.Count > 1000 || (options.Count == 0 && options.Until.IsZero()) {
		return nil, router.Errorf(422, "recurrence needs COUNT up to 1000 or UNTIL within ten years")
	}
	horizon := start.AddDate(10, 0, 0)
	if !options.Until.IsZero() && (options.Until.After(horizon) || options.Until.Before(start)) {
		return nil, router.Errorf(422, "recurrence UNTIL must fall within ten years")
	}
	options.Dtstart = start
	if options.Until.IsZero() {
		options.Until = horizon
	}
	targetCount := options.Count
	options.Count = 0
	base, err := rrule.NewRRule(*options)
	if err != nil {
		return nil, router.Errorf(422, "invalid recurrence rule")
	}
	hour, minute, second := start.Hour(), start.Minute(), start.Second()
	if len(options.Byhour) > 0 {
		hour = options.Byhour[0]
	}
	if len(options.Byminute) > 0 {
		minute = options.Byminute[0]
	}
	if len(options.Bysecond) > 0 {
		second = options.Bysecond[0]
	}
	rule := &Recurrence{rule: base, count: targetCount, hour: hour, minute: minute, second: second, allDay: allDay}
	next := rule.Iterator()
	count := 0
	for {
		_, ok := next()
		if !ok {
			break
		}
		count++
		if count > 1000 {
			return nil, router.Errorf(422, "recurrence exceeds 1000 occurrences")
		}
	}
	if targetCount > 0 && count < targetCount {
		return nil, router.Errorf(422, "recurrence COUNT must finish within ten years")
	}
	if count == 0 {
		return nil, router.Errorf(422, "recurrence has no occurrences")
	}
	return rule, nil
}
func Validate(in schema.EventInput) (schema.EventInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	if err := in.Validate(); err != nil {
		return in, err
	}
	start, _, err := timing(in)
	if err != nil {
		return in, err
	}
	if _, err = Rule(text(in.Rrule), start, in.AllDay); err != nil {
		return in, err
	}
	return in, nil
}
func eventInput(e q.CalendarEvent) schema.EventInput {
	return schema.EventInput{Title: e.Title, AllDay: e.AllDay, StartDate: &e.StartDate, EndDate: &e.EndDate, StartsAt: e.StartsAt, EndsAt: e.EndsAt, TimeZone: e.TimeZone, Rrule: &e.Rrule, Attendees: []string{}}
}
func key(t time.Time, allDay bool) string {
	if allDay {
		return t.Format("2006-01-02")
	}
	return t.UTC().Format(time.RFC3339)
}
func starts(e q.CalendarEvent) ([]time.Time, time.Duration, error) {
	start, end, err := timing(eventInput(e))
	if err != nil {
		return nil, 0, err
	}
	rule, err := Rule(e.Rrule, start, e.AllDay)
	if err != nil {
		return nil, 0, err
	}
	if rule == nil {
		return []time.Time{start}, end.Sub(start), nil
	}
	out := []time.Time{}
	next := rule.Iterator()
	for {
		v, ok := next()
		if !ok {
			break
		}
		out = append(out, v)
		if len(out) > 1000 {
			return nil, 0, errors.New("recurrence expansion limit")
		}
	}
	return out, end.Sub(start), nil
}
func ValidateException(e q.CalendarEvent, in schema.ExceptionInput) error {
	occurrences, _, err := starts(e)
	if err != nil {
		return err
	}
	found := false
	for _, t := range occurrences {
		if key(t, e.AllDay) == in.InstanceKey {
			found = true
			break
		}
	}
	if !found {
		return router.Errorf(422, "exception must name an occurrence of this series")
	}
	if in.Cancelled {
		return nil
	}
	hasTimes := in.StartsAt != nil || in.EndsAt != nil || in.StartDate != nil || in.EndDate != nil
	if hasTimes {
		base := eventInput(e)
		base.StartsAt, base.EndsAt = in.StartsAt, in.EndsAt
		base.StartDate, base.EndDate = in.StartDate, in.EndDate
		_, _, err = timing(base)
	}
	return err
}
func Instances(e q.CalendarEvent, color string, exceptions []q.EventException, from, to time.Time) ([]schema.EventInstance, error) {
	out := []schema.EventInstance{}
	if e.Cancelled {
		return out, nil
	}
	occurrences, duration, err := starts(e)
	if err != nil {
		return nil, err
	}
	overrides := map[string]q.EventException{}
	for _, ex := range exceptions {
		overrides[ex.InstanceKey] = ex
	}
	for _, start := range occurrences {
		instanceKey := key(start, e.AllDay)
		end := start.Add(duration)
		title := e.Title
		if ex, ok := overrides[instanceKey]; ok {
			if ex.Cancelled {
				continue
			}
			if ex.Title != nil {
				title = *ex.Title
			}
			if e.AllDay && ex.StartDate != nil {
				start, err = time.Parse("2006-01-02", *ex.StartDate)
				if err == nil && ex.EndDate != nil {
					end, err = time.Parse("2006-01-02", *ex.EndDate)
				}
				if err != nil {
					return nil, err
				}
			} else if !e.AllDay && ex.StartsAt != nil && ex.EndsAt != nil {
				start, end = *ex.StartsAt, *ex.EndsAt
			}
		}
		if !end.After(from) || !start.Before(to) {
			continue
		}
		out = append(out, schema.EventInstance{EventID: e.ID, CalendarID: e.CalendarID, InstanceKey: instanceKey, Title: title, Location: e.Location, AllDay: e.AllDay, Start: key(start, e.AllDay), End: key(end, e.AllDay), Color: color, Sequence: int(e.Sequence)})
	}
	return out, nil
}
