package calendar

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ICSReply struct {
	UID, Email, Response, Recurrence string
	Sequence                         int
}

// A REPLY has no required DTSTART or SUMMARY. Accept one event and one attendee,
// keeping the reply identity and sequence separate from event import.
func ParseReplyICS(raw string) (ICSReply, error) {
	out := ICSReply{}
	if len(raw) > 100000 || !utf8.ValidString(raw) {
		return out, fmt.Errorf("invalid reply size or encoding")
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := []string{}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(lines) == 0 {
				return out, fmt.Errorf("invalid folding")
			}
			lines[len(lines)-1] += line[1:]
		} else if line != "" {
			lines = append(lines, line)
		}
	}
	depth := []string{}
	method := ""
	count := 0
	calendars := 0
	seen := map[string]bool{}
	for _, line := range lines {
		p, err := parseProperty(line)
		if err != nil {
			return out, err
		}
		if p.name == "BEGIN" {
			component := strings.ToUpper(p.value)
			if len(depth) == 0 && component != "VCALENDAR" || len(depth) == 1 && component != "VEVENT" || len(depth) > 1 {
				return out, fmt.Errorf("unsupported reply component")
			}
			if component == "VCALENDAR" {
				calendars++
				if calendars > 1 {
					return out, fmt.Errorf("one calendar required")
				}
			}
			depth = append(depth, component)
			if component == "VEVENT" {
				count++
				if count > 1 {
					return out, fmt.Errorf("one reply event required")
				}
			}
			continue
		}
		if p.name == "END" {
			if len(depth) == 0 || depth[len(depth)-1] != strings.ToUpper(p.value) {
				return out, fmt.Errorf("unbalanced reply")
			}
			depth = depth[:len(depth)-1]
			continue
		}
		if len(depth) == 1 && p.name == "METHOD" {
			if method != "" {
				return out, fmt.Errorf("duplicate method")
			}
			method = strings.ToUpper(p.value)
		}
		if len(depth) != 2 {
			continue
		}
		switch p.name {
		case "UID", "SEQUENCE", "ATTENDEE", "RECURRENCE-ID":
			if seen[p.name] {
				return out, fmt.Errorf("duplicate reply property")
			}
			seen[p.name] = true
		}
		switch p.name {
		case "UID":
			out.UID = unescape(p.value)
		case "SEQUENCE":
			out.Sequence, err = strconv.Atoi(p.value)
			if err != nil || out.Sequence < 0 {
				return out, fmt.Errorf("invalid sequence")
			}
		case "ATTENDEE":
			if !strings.HasPrefix(strings.ToLower(p.value), "mailto:") {
				return out, fmt.Errorf("email attendee required")
			}
			out.Email = strings.ToLower(strings.TrimPrefix(strings.ToLower(p.value), "mailto:"))
			out.Response = strings.ToLower(p.params["PARTSTAT"])
		case "RECURRENCE-ID":
			out.Recurrence = p.value
		}
	}
	if len(depth) != 0 || method != "REPLY" || count != 1 || out.UID == "" || out.Email == "" || !seen["SEQUENCE"] {
		return out, fmt.Errorf("METHOD:REPLY, UID, SEQUENCE and one attendee required")
	}
	if out.Response != "accepted" && out.Response != "declined" && out.Response != "tentative" {
		return out, fmt.Errorf("invalid attendance response")
	}
	return out, nil
}
