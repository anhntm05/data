package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DateOrder decides how ambiguous dates like 03/04/2026 are read.
type DateOrder int

const (
	DMY DateOrder = iota // 03/04/2026 = 3 April
	MDY                  // 03/04/2026 = 4 March
)

var (
	reNumber = regexp.MustCompile(`^[+-]?(\d+)(?:\.(\d*))?$`)
	timePart = `(?:[ T]+(\d{1,2}):(\d{2})(?::(\d{2})(?:[.,](\d+))?)?\s*([AaPp][Mm])?)?\s*(?:Z|[+-]\d{2}:?\d{2})?`
	reISO    = regexp.MustCompile(`^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})` + timePart + `$`)
	reDMY    = regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4}|\d{2})` + timePart + `$`)
	reMon    = regexp.MustCompile(`^(\d{1,2})[-/ ]([A-Za-z]{3})[A-Za-z]*[-/ ,]+(\d{4}|\d{2})` + timePart + `$`)
	months   = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
)

// IsNull reports whether a cell counts as empty: blank, whitespace or NULL.
func IsNull(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.EqualFold(s, "null")
}

// normNumber canonicalizes a plain decimal string without going through
// float64, so 12435678654.04001 and 12435678654.03001 stay different.
// Values with leading zeros (e.g. codes like 007) are not treated as numbers.
func normNumber(s string) (string, bool) {
	m := reNumber.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	intPart, frac := m[1], strings.TrimRight(m[2], "0")
	if len(intPart) > 1 && intPart[0] == '0' {
		return "", false
	}
	neg := s[0] == '-'
	out := intPart
	if frac != "" {
		out += "." + frac
	}
	if out == "0" {
		neg = false
	}
	if neg {
		out = "-" + out
	}
	return out, true
}

func normDate(s string, order DateOrder) (string, bool) {
	var y, mo, d int
	var tm []string
	if m := reISO.FindStringSubmatch(s); m != nil {
		y, mo, d = atoi(m[1]), atoi(m[2]), atoi(m[3])
		tm = m[4:]
	} else if m := reMon.FindStringSubmatch(s); m != nil {
		mon, ok := months[strings.ToLower(m[2])]
		if !ok {
			return "", false
		}
		d, mo, y = atoi(m[1]), mon, year(m[3])
		tm = m[4:]
	} else if m := reDMY.FindStringSubmatch(s); m != nil {
		a, b := atoi(m[1]), atoi(m[2])
		y = year(m[3])
		switch {
		case a > 12 && b <= 12:
			d, mo = a, b
		case b > 12 && a <= 12:
			mo, d = a, b
		case order == MDY:
			mo, d = a, b
		default:
			d, mo = a, b
		}
		tm = m[4:]
	} else {
		return "", false
	}
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return "", false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d {
		return "", false
	}
	out := t.Format("2006-01-02")
	if tm[0] == "" {
		return out, true
	}
	hh, mm, ss := atoi(tm[0]), atoi(tm[1]), atoi(tm[2])
	if ap := strings.ToLower(tm[4]); ap != "" {
		if hh < 1 || hh > 12 {
			return "", false
		}
		if ap == "pm" && hh != 12 {
			hh += 12
		} else if ap == "am" && hh == 12 {
			hh = 0
		}
	}
	if hh > 23 || mm > 59 || ss > 59 {
		return "", false
	}
	frac := strings.TrimRight(tm[3], "0")
	if hh == 0 && mm == 0 && ss == 0 && frac == "" {
		// 2026-10-26 00:00:00 is treated the same as 2026-10-26.
		return out, true
	}
	out += fmt.Sprintf(" %02d:%02d:%02d", hh, mm, ss)
	if frac != "" {
		out += "." + frac
	}
	return out, true
}

// LooksAmbiguousDate reports a date like 03/04/2026 whose day and month
// could be swapped, so the user has to say which order the file uses.
func LooksAmbiguousDate(s string) bool {
	m := reDMY.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return false
	}
	a, b := atoi(m[1]), atoi(m[2])
	return a <= 12 && b <= 12 && a != b
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func year(s string) int {
	y := atoi(s)
	if len(s) == 2 {
		if y < 70 {
			return 2000 + y
		}
		return 1900 + y
	}
	return y
}
