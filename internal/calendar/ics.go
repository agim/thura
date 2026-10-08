package calendar

import (
	"fmt"
	"github.com/agim/lidza/pkg/router"
	"net/mail"
	"strconv"
	"strings"
	q "thura/db/queries/gen"
	"thura/schema"
	"time"
	"unicode/utf8"
)

type Imported struct {
	UID, Organizer, Instance string
	HasTitle                 bool
	Sequence                 int
	Cancelled                bool
	Input                    schema.EventInput
	Exdates                  []string
	Responses                map[string]q.Attendance
}
type property struct {
	name, value string
	params      map[string]string
}

func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
func escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, ";", "\\;")
	return strings.ReplaceAll(s, ",", "\\,")
}
func parseProperty(line string) (property, error) {
	colon := -1
	quoted := false
	for i, c := range line {
		if c == '"' {
			quoted = !quoted
		}
		if c == ':' && !quoted {
			colon = i
			break
		}
	}
	if colon < 1 {
		return property{}, fmt.Errorf("invalid calendar property")
	}
	header := line[:colon]
	parts := []string{}
	start := 0
	quoted = false
	for i, c := range header {
		if c == '"' {
			quoted = !quoted
		}
		if c == ';' && !quoted {
			parts = append(parts, header[start:i])
			start = i + 1
		}
	}
	parts = append(parts, header[start:])
	p := property{name: strings.ToUpper(parts[0]), value: line[colon+1:], params: map[string]string{}}
	for _, s := range parts[1:] {
		kv := strings.SplitN(s, "=", 2)
		if len(kv) != 2 {
			return p, fmt.Errorf("invalid calendar parameter")
		}
		p.params[strings.ToUpper(kv[0])] = strings.Trim(kv[1], "\"")
	}
	return p, nil
}
func propertyTime(p property, zone string) (time.Time, bool, string, error) {
	allDay := p.params["VALUE"] == "DATE" || len(p.value) == 8
	if allDay {
		t, err := time.Parse("20060102", p.value)
		return t, true, zone, err
	}
	if strings.HasSuffix(p.value, "Z") {
		t, err := time.Parse("20060102T150405Z", p.value)
		return t, false, "UTC", err
	}
	if p.params["TZID"] != "" {
		zone = p.params["TZID"]
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, false, zone, err
	}
	t, err := time.ParseInLocation("20060102T150405", p.value, loc)
	return t, false, zone, err
}
func ParseICS(raw []byte, defaultZone string) ([]Imported, string, error) {
	if len(raw) > 1<<20 || strings.ContainsRune(string(raw), 0) {
		return nil, "", router.Errorf(413, "calendar import exceeds one megabyte or contains invalid bytes")
	}
	if _, err := time.LoadLocation(defaultZone); err != nil {
		return nil, "", router.Errorf(422, "use a valid import timezone")
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	unfolded := []string{}
	for _, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(unfolded) == 0 {
				return nil, "", router.Errorf(422, "invalid folded calendar")
			}
			unfolded[len(unfolded)-1] += line[1:]
		} else if line != "" {
			unfolded = append(unfolded, line)
		}
	}
	events := []Imported{}
	method := ""
	inside := false
	calendar := false
	var props []property
	depth := 0
	stack := []string{}
	for _, line := range unfolded {
		p, err := parseProperty(line)
		if err != nil {
			return nil, "", router.Errorf(422, "invalid calendar syntax")
		}
		switch p.name {
		case "BEGIN":
			stack = append(stack, p.value)
			depth++
			if depth > 8 {
				return nil, "", router.Errorf(422, "calendar nesting limit")
			}
			if p.value == "VCALENDAR" {
				calendar = true
			}
			if p.value == "VEVENT" {
				if inside {
					return nil, "", router.Errorf(422, "nested event")
				}
				inside = true
				props = nil
			}
		case "END":
			if len(stack) == 0 || stack[len(stack)-1] != p.value {
				return nil, "", router.Errorf(422, "mismatched calendar component")
			}
			stack = stack[:len(stack)-1]
			if p.value == "VEVENT" {
				if !inside {
					return nil, "", router.Errorf(422, "unmatched event end")
				}
				e, err := parseEvent(props, defaultZone, method == "CANCEL")
				if err != nil {
					return nil, "", err
				}
				events = append(events, e)
				if len(events) > 200 {
					return nil, "", router.Errorf(422, "import at most 200 events at once")
				}
				inside = false
			}
			depth--
			if depth < 0 {
				return nil, "", router.Errorf(422, "unmatched calendar component")
			}
		default:
			if inside && len(stack) > 0 && stack[len(stack)-1] == "VEVENT" {
				props = append(props, p)
			} else if p.name == "METHOD" && len(stack) > 0 && stack[len(stack)-1] == "VCALENDAR" {
				method = strings.ToUpper(p.value)
			}
		}
	}
	if !calendar || inside || depth != 0 || len(events) == 0 {
		return nil, "", router.Errorf(422, "calendar must contain complete events")
	}
	if method != "" && method != "PUBLISH" && method != "REQUEST" && method != "CANCEL" {
		return nil, "", router.Errorf(422, "this import accepts events, requests and cancellations")
	}
	return events, method, nil
}
func parseEvent(props []property, zone string, forceCancel bool) (Imported, error) {
	e := Imported{Cancelled: forceCancel, Input: schema.EventInput{TimeZone: zone, Attendees: []string{}}, Responses: map[string]q.Attendance{}}
	var start, end *property
	var duration string
	for _, p := range props {
		switch p.name {
		case "UID":
			e.UID = unescape(p.value)
		case "SEQUENCE":
			v, err := strconv.Atoi(p.value)
			if err != nil || v < 0 || v > 1000000000 {
				return e, router.Errorf(422, "invalid event sequence")
			}
			e.Sequence = v
		case "SUMMARY":
			e.Input.Title = unescape(p.value)
			e.HasTitle = true
		case "DESCRIPTION":
			v := unescape(p.value)
			e.Input.Description = &v
		case "LOCATION":
			v := unescape(p.value)
			e.Input.Location = &v
		case "DTSTART":
			v := p
			start = &v
		case "DTEND":
			v := p
			end = &v
		case "DURATION":
			duration = p.value
		case "RRULE":
			v := p.value
			e.Input.Rrule = &v
		case "STATUS":
			e.Cancelled = e.Cancelled || p.value == "CANCELLED"
		case "ORGANIZER":
			a, err := mail.ParseAddress(strings.TrimPrefix(strings.TrimPrefix(p.value, "mailto:"), "MAILTO:"))
			if err != nil {
				return e, router.Errorf(422, "invalid organizer")
			}
			e.Organizer = strings.ToLower(a.Address)
		case "ATTENDEE":
			a, err := mail.ParseAddress(strings.TrimPrefix(strings.TrimPrefix(p.value, "mailto:"), "MAILTO:"))
			if err != nil {
				return e, router.Errorf(422, "invalid attendee")
			}
			email := strings.ToLower(a.Address)
			e.Input.Attendees = append(e.Input.Attendees, email)
			response := q.Attendance(strings.ToLower(strings.ReplaceAll(p.params["PARTSTAT"], "-", "")))
			switch response {
			case "accepted", "tentative", "declined":
				e.Responses[email] = response
			default:
				e.Responses[email] = q.AttendanceNeedsaction
			}
		case "RECURRENCE-ID":
			t, allDay, _, err := propertyTime(p, zone)
			if err != nil {
				return e, router.Errorf(422, "invalid recurrence id")
			}
			e.Instance = key(t, allDay)
		case "EXDATE":
			for _, value := range strings.Split(p.value, ",") {
				part := p
				part.value = value
				t, allDay, _, err := propertyTime(part, zone)
				if err != nil {
					return e, router.Errorf(422, "invalid exception date")
				}
				e.Exdates = append(e.Exdates, key(t, allDay))
				if len(e.Exdates) > 1000 {
					return e, router.Errorf(422, "too many exceptions")
				}
			}
		}
	}
	if e.UID == "" || len(e.UID) > 500 || strings.ContainsAny(e.UID, "\r\n") {
		return e, router.Errorf(422, "event UID required")
	}
	if _, addressErr := attendees(e.Input.Attendees); addressErr != nil {
		return e, addressErr
	}
	if start == nil && e.Cancelled {
		return e, nil
	}
	if start == nil {
		return e, router.Errorf(422, "event DTSTART required")
	}
	first, allDay, timeZone, err := propertyTime(*start, zone)
	if err != nil {
		return e, router.Errorf(422, "unsupported DTSTART or timezone")
	}
	e.Input.AllDay = allDay
	e.Input.TimeZone = timeZone
	last := first.Add(time.Hour)
	if allDay {
		last = first.AddDate(0, 0, 1)
	}
	if end != nil {
		var endDay bool
		last, endDay, _, err = propertyTime(*end, timeZone)
		if err != nil || endDay != allDay {
			return e, router.Errorf(422, "DTEND must use the same date/time kind as DTSTART")
		}
	} else if duration != "" {
		d, derr := parseDuration(duration)
		if derr != nil {
			return e, router.Errorf(422, "unsupported event duration")
		}
		last = first.Add(d)
	}
	if allDay {
		a, b := first.Format("2006-01-02"), last.Format("2006-01-02")
		e.Input.StartDate, e.Input.EndDate = &a, &b
	} else {
		e.Input.StartsAt, e.Input.EndsAt = &first, &last
	}
	// Floating recurrence identifiers/exdates use DTSTART's timezone,
	// regardless of their position among the event properties.
	e.Exdates = nil
	for _, p := range props {
		if p.name == "RECURRENCE-ID" {
			t, day, _, parseErr := propertyTime(p, timeZone)
			if parseErr != nil {
				return e, router.Errorf(422, "invalid recurrence id")
			}
			e.Instance = key(t, day)
		}
		if p.name == "EXDATE" {
			for _, value := range strings.Split(p.value, ",") {
				part := p
				part.value = value
				t, day, _, parseErr := propertyTime(part, timeZone)
				if parseErr != nil {
					return e, router.Errorf(422, "invalid exception date")
				}
				e.Exdates = append(e.Exdates, key(t, day))
			}
		}
	}
	if !e.HasTitle && e.Instance != "" {
		e.Input.Title = "Occurrence"
	}
	e.Input, err = Validate(e.Input)
	if err != nil {
		return e, err
	}
	if _, err = attendees(e.Input.Attendees); err != nil {
		return e, err
	}
	return e, nil
}
func parseDuration(raw string) (time.Duration, error) {
	if !strings.HasPrefix(raw, "P") {
		return 0, fmt.Errorf("invalid duration")
	}
	var total time.Duration
	number := ""
	clock := false
	for _, c := range raw[1:] {
		if c == 'T' {
			clock = true
			continue
		}
		if c >= '0' && c <= '9' {
			number += string(c)
			if len(number) > 6 {
				return 0, fmt.Errorf("duration too large")
			}
			continue
		}
		v, err := strconv.Atoi(number)
		if err != nil {
			return 0, err
		}
		number = ""
		switch c {
		case 'W':
			if clock {
				return 0, fmt.Errorf("invalid duration")
			}
			total += time.Duration(v) * 7 * 24 * time.Hour
		case 'D':
			if clock {
				return 0, fmt.Errorf("invalid duration")
			}
			total += time.Duration(v) * 24 * time.Hour
		case 'H':
			if !clock {
				return 0, fmt.Errorf("invalid duration")
			}
			total += time.Duration(v) * time.Hour
		case 'M':
			if !clock {
				return 0, fmt.Errorf("invalid duration")
			}
			total += time.Duration(v) * time.Minute
		case 'S':
			if !clock {
				return 0, fmt.Errorf("invalid duration")
			}
			total += time.Duration(v) * time.Second
		default:
			return 0, fmt.Errorf("invalid duration")
		}
	}
	if number != "" || total <= 0 || total > 366*24*time.Hour {
		return 0, fmt.Errorf("invalid duration")
	}
	return total, nil
}
func fold(line string) string {
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		n := limit
		for n > 0 && !utf8.RuneStart(line[n]) {
			n--
		}
		b.WriteString(line[:n])
		b.WriteString("\r\n ")
		line = line[n:]
		limit = 74
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}
func offset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf("%s%02d%02d", sign, seconds/3600, seconds%3600/60)
}
func timeZoneICS(name string) string {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return ""
	}
	var b strings.Builder
	write := func(s string) { b.WriteString(fold(s)) }
	write("BEGIN:VTIMEZONE")
	write("TZID:" + name)
	current := time.Date(2000, 1, 1, 0, 0, 0, 0, loc)
	abbreviation, previous := current.Zone()
	kind := "STANDARD"
	if current.IsDST() {
		kind = "DAYLIGHT"
	}
	write("BEGIN:" + kind)
	write("DTSTART:20000101T000000")
	write("TZOFFSETFROM:" + offset(previous))
	write("TZOFFSETTO:" + offset(previous))
	write("TZNAME:" + escape(abbreviation))
	write("END:" + kind)
	for n := 0; n < 300; n++ {
		_, bound := current.ZoneBounds()
		if bound.IsZero() || bound.Year() > 2100 || !bound.After(current) {
			break
		}
		after := bound.In(loc)
		abbreviation, next := after.Zone()
		kind = "STANDARD"
		if after.IsDST() {
			kind = "DAYLIGHT"
		}
		write("BEGIN:" + kind)
		write("DTSTART:" + bound.In(time.FixedZone("previous", previous)).Format("20060102T150405"))
		write("TZOFFSETFROM:" + offset(previous))
		write("TZOFFSETTO:" + offset(next))
		write("TZNAME:" + escape(abbreviation))
		write("END:" + kind)
		previous = next
		current = after.Add(time.Second)
	}
	write("END:VTIMEZONE")
	return b.String()
}
func eventICS(e q.CalendarEvent, attendees []q.EventAttendee, exceptions []q.EventException, meetingURL string) (string, error) {
	var b strings.Builder
	write := func(s string) { b.WriteString(fold(s)) }
	start, end, err := timing(eventInput(e))
	if err != nil {
		return "", err
	}
	write("BEGIN:VEVENT")
	write("UID:" + escape(e.Uid))
	write("DTSTAMP:" + e.UpdatedAt.UTC().Format("20060102T150405Z"))
	write("SEQUENCE:" + strconv.Itoa(int(e.Sequence)))
	write("SUMMARY:" + escape(e.Title))
	write("DESCRIPTION:" + escape(e.Description))
	write("LOCATION:" + escape(e.Location))
	if meetingURL != "" {
		write("URL:" + meetingURL)
	}
	write("ORGANIZER:mailto:" + e.Organizer)
	if e.AllDay {
		write("DTSTART;VALUE=DATE:" + start.Format("20060102"))
		write("DTEND;VALUE=DATE:" + end.Format("20060102"))
	} else {
		write("DTSTART;TZID=" + e.TimeZone + ":" + start.Format("20060102T150405"))
		write("DTEND;TZID=" + e.TimeZone + ":" + end.Format("20060102T150405"))
	}
	if e.Rrule != "" {
		write("RRULE:" + e.Rrule)
	}
	if e.Cancelled {
		write("STATUS:CANCELLED")
	}
	for _, a := range attendees {
		response := strings.ToUpper(string(a.Response))
		if response == "NEEDSACTION" {
			response = "NEEDS-ACTION"
		}
		write("ATTENDEE;PARTSTAT=" + response + ";RSVP=TRUE:mailto:" + a.Email)
	}
	for _, ex := range exceptions {
		if ex.Cancelled {
			if e.AllDay {
				t, parseErr := time.Parse("2006-01-02", ex.InstanceKey)
				if parseErr != nil {
					return "", parseErr
				}
				write("EXDATE;VALUE=DATE:" + t.Format("20060102"))
			} else {
				t, parseErr := time.Parse(time.RFC3339, ex.InstanceKey)
				if parseErr != nil {
					return "", parseErr
				}
				write("EXDATE:" + t.UTC().Format("20060102T150405Z"))
			}
		}
	}
	write("END:VEVENT")
	for _, ex := range exceptions {
		if ex.Cancelled {
			continue
		}
		var t time.Time
		if e.AllDay {
			t, err = time.Parse("2006-01-02", ex.InstanceKey)
		} else {
			t, err = time.Parse(time.RFC3339, ex.InstanceKey)
		}
		if err != nil {
			return "", err
		}
		_, duration, durationErr := starts(e)
		if durationErr != nil {
			return "", durationErr
		}
		replacement := e
		replacement.Rrule = ""
		if ex.Title != nil {
			replacement.Title = *ex.Title
		}
		if e.AllDay {
			replacement.StartDate, replacement.EndDate = t.Format("2006-01-02"), t.Add(duration).Format("2006-01-02")
			if ex.StartDate != nil {
				replacement.StartDate = *ex.StartDate
			}
			if ex.EndDate != nil {
				replacement.EndDate = *ex.EndDate
			}
		} else {
			a, z := t, t.Add(duration)
			replacement.StartsAt, replacement.EndsAt = &a, &z
			if ex.StartsAt != nil {
				replacement.StartsAt = ex.StartsAt
			}
			if ex.EndsAt != nil {
				replacement.EndsAt = ex.EndsAt
			}
		}
		component, componentErr := eventICS(replacement, attendees, nil, meetingURL)
		if componentErr != nil {
			return "", componentErr
		}
		recurrence := "RECURRENCE-ID:" + t.UTC().Format("20060102T150405Z")
		if e.AllDay {
			recurrence = "RECURRENCE-ID;VALUE=DATE:" + t.Format("20060102")
		}
		component = strings.Replace(component, "BEGIN:VEVENT\r\n", "BEGIN:VEVENT\r\n"+fold(recurrence), 1)
		b.WriteString(component)
	}
	return b.String(), nil
}
func ExportICS(events []q.CalendarEvent, attendees map[string][]q.EventAttendee, exceptions map[string][]q.EventException, method string, meetingLinks ...map[string]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Thura//Calendar 1.0//EN\r\nCALSCALE:GREGORIAN\r\n")
	if method != "" {
		b.WriteString(fold("METHOD:" + method))
	}
	zones := map[string]bool{}
	for _, e := range events {
		if !e.AllDay && !zones[e.TimeZone] {
			zones[e.TimeZone] = true
			b.WriteString(timeZoneICS(e.TimeZone))
		}
		meetingURL := ""
		if len(meetingLinks) > 0 {
			meetingURL = meetingLinks[0][e.ID]
		}
		component, err := eventICS(e, attendees[e.ID], exceptions[e.ID], meetingURL)
		if err != nil {
			return nil, err
		}
		b.WriteString(component)
		if b.Len() > 8<<20 {
			return nil, router.Errorf(413, "calendar export exceeds eight megabytes")
		}
	}
	b.WriteString("END:VCALENDAR\r\n")
	return []byte(b.String()), nil
}
