package extract

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// bangkok anchors every timestamp this package produces to UTC+7. A fixed
// zone avoids depending on tzdata in the container image.
var bangkok = time.FixedZone("ICT", 7*60*60)

var (
	// dateRe matches an explicit numeric date, day/month/year order as
	// written in Thailand ("20/09/2026", "20.09.2569").
	dateRe = regexp.MustCompile(`(\d{1,2})[./](\d{1,2})[./](\d{2,4})`)
	// clockRe matches an explicit clock time; Thai usage separates hour and
	// minute with either a dot or a colon ("00.25" = 00:25, 24-hour clock).
	clockRe = regexp.MustCompile(`(\d{1,2})[.:](\d{2})`)
	// hourRe matches an hour tied to an explicit clock marker but without
	// minutes: "เวลา 9" / "เวลา 9 น." / "21 น.".
	hourRe = regexp.MustCompile(`เวลา\s*(\d{1,2})|(\d{1,2})\s*น\.`)
)

// Refine merges deterministic parsing of explicit numeric dates and clock
// times with what the LLM extracted. Models misread formats like
// "วันที่ 20/09/2026 เวลา 00.25" (and near-midnight times) often enough that
// anything written explicitly in the text overrides the LLM; parts the text
// leaves implicit (the day for a bare clock time, the clock for a bare date)
// come from the LLM result or from roll-forward rules. When the text has
// nothing explicit, llmAt is returned unchanged.
func Refine(text string, llmAt, now time.Time) time.Time {
	now = now.In(bangkok)

	rest := text
	var year, month, day int
	hasDate := false
	if loc := dateRe.FindStringSubmatchIndex(rest); loc != nil {
		d, _ := strconv.Atoi(rest[loc[2]:loc[3]])
		m, _ := strconv.Atoi(rest[loc[4]:loc[5]])
		y := normalizeYear(rest[loc[6]:loc[7]], detectEra(text))
		if d >= 1 && d <= 31 && m >= 1 && m <= 12 {
			day, month, year, hasDate = d, m, y, true
			// Cut the date out so its digits can't be misread as a clock.
			rest = rest[:loc[0]] + rest[loc[1]:]
		}
	}
	hour, minute, hasClock := findClock(rest)
	dayOffset, hasRelDay := relativeDay(text)

	if !hasDate && !hasClock && !hasRelDay {
		return llmAt
	}

	base := now
	switch {
	case hasDate:
		base = time.Date(year, time.Month(month), day, 0, 0, 0, 0, bangkok)
	case hasRelDay:
		base = now.AddDate(0, 0, dayOffset)
	case !llmAt.IsZero():
		base = llmAt.In(bangkok)
	}
	if !hasClock {
		if llmAt.IsZero() {
			// A date but no time of day: let the flow ask for it.
			return time.Time{}
		}
		l := llmAt.In(bangkok)
		hour, minute = l.Hour(), l.Minute()
	}

	cand := time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, bangkok)
	// A bare clock time that already passed today means the next occurrence
	// ("เตือน 00.25" typed at 23:50 is tonight, not 23 hours ago).
	if !hasDate && !hasRelDay && !cand.After(now) {
		cand = cand.AddDate(0, 0, 1)
	}
	return cand
}

func findClock(s string) (hour, minute int, ok bool) {
	for _, m := range clockRe.FindAllStringSubmatch(s, -1) {
		h, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if h <= 23 && mm <= 59 {
			return h, mm, true
		}
	}
	for _, m := range hourRe.FindAllStringSubmatch(s, -1) {
		digits := m[1]
		if digits == "" {
			digits = m[2]
		}
		if h, err := strconv.Atoi(digits); err == nil && h <= 23 {
			return h, 0, true
		}
	}
	return 0, 0, false
}

// relativeDay recognizes the unambiguous relative-day words so an explicit
// clock time next to them lands on the right day.
func relativeDay(text string) (int, bool) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "มะรืน"):
		return 2, true
	case strings.Contains(lower, "พรุ่งนี้"), strings.Contains(lower, "tomorrow"):
		return 1, true
	case strings.Contains(lower, "วันนี้"), strings.Contains(lower, "คืนนี้"),
		strings.Contains(lower, "today"), strings.Contains(lower, "tonight"):
		return 0, true
	}
	return 0, false
}

type era int

const (
	eraAuto era = iota // no marker: infer from the year's magnitude
	eraBE              // "พ.ศ." written in the text
	eraCE              // "ค.ศ." written in the text
)

// detectEra looks for an explicit Thai era marker anywhere in the text:
// พ.ศ. (พุทธศักราช, Buddhist Era) or ค.ศ. (คริสต์ศักราช, Christian Era).
func detectEra(text string) era {
	switch {
	case strings.Contains(text, "พ.ศ"):
		return eraBE
	case strings.Contains(text, "ค.ศ"):
		return eraCE
	}
	return eraAuto
}

// normalizeYear turns what the user wrote into a Christian-Era year. Both
// eras are supported in 4-digit and 2-digit form: "2026" and "2569" (and,
// with the century dropped, "26" and "69") all mean 2026. An explicit
// พ.ศ./ค.ศ. marker wins; otherwise the magnitude decides (>= 2400 must be
// Buddhist Era, a 2-digit year >= 60 only makes sense as one).
func normalizeYear(s string, hint era) int {
	y, _ := strconv.Atoi(s)
	if y < 100 {
		switch {
		case hint == eraCE:
			return y + 2000
		case hint == eraBE:
			return y + 2500 - 543
		case y >= 60:
			return y + 2500 - 543
		default:
			return y + 2000
		}
	}
	switch {
	case hint == eraBE:
		return y - 543
	case hint == eraCE:
		return y
	case y >= 2400:
		return y - 543
	default:
		return y
	}
}
