package extract

import (
	"testing"
	"time"
)

// These cases target branches of Refine (defined in explicit.go) not already
// exercised by TestRefine in extract_test.go: the มะรืน/today relative-day
// words, the "<digit> น." clock-marker alternation, findClock skipping
// out-of-range matches before an in-range one, and an invalid explicit date
// falling back to the LLM's guess untouched.
func TestRefineExplicitEdgeCases(t *testing.T) {
	at := func(y int, mo time.Month, d, h, mi int) time.Time {
		return time.Date(y, mo, d, h, mi, 0, 0, bangkok)
	}
	noon := at(2026, 7, 19, 12, 0)

	cases := []struct {
		name  string
		text  string
		llmAt time.Time
		now   time.Time
		want  time.Time
	}{
		{
			name: "มะรืน (day after tomorrow) with explicit clock",
			text: "มะรืนนี้ 10.00 ไปทำฟัน",
			now:  noon,
			want: at(2026, 7, 21, 10, 0),
		},
		{
			name: "วันนี้/คืนนี้ (today) keeps the same date",
			text: "คืนนี้ 22.00 ดูหนัง",
			now:  noon,
			want: at(2026, 7, 19, 22, 0),
		},
		{
			name: "english today/tonight relative words",
			text: "reminder tonight 21.15 take medicine",
			now:  noon,
			want: at(2026, 7, 19, 21, 15),
		},
		{
			name:  "digit-then-marker hour without เวลา keyword",
			text:  "นัด 15 น. พรุ่งนี้",
			llmAt: at(2026, 7, 19, 15, 0),
			now:   noon,
			want:  at(2026, 7, 20, 15, 0),
		},
		{
			// now is set before 9:30 so the bare-clock rollover rule (a time
			// already past today rolls to tomorrow) doesn't shift the date,
			// keeping this case focused on the skip-invalid-match behavior.
			name: "findClock skips an out-of-range match and uses the next one",
			text: "ผิดเวลา 88.99 แต่จริงคือ 9.30 นัดหมอ",
			now:  at(2026, 7, 19, 6, 0),
			want: at(2026, 7, 19, 9, 30),
		},
		{
			name:  "invalid explicit date (bad day and month) falls back to the LLM",
			text:  "วันที่ 99/88/2026 ไปเที่ยว",
			llmAt: at(2026, 8, 1, 9, 0),
			now:   noon,
			want:  at(2026, 8, 1, 9, 0),
		},
		{
			name: "nothing explicit at all returns the zero-value LLM guess unchanged",
			text: "ไปเที่ยว",
			now:  noon,
			want: time.Time{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Refine(tc.text, tc.llmAt, tc.now)
			if !got.Equal(tc.want) {
				t.Errorf("Refine(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestDetectEra(t *testing.T) {
	cases := []struct {
		text string
		want era
	}{
		{"วันที่ 20/09/2569 พ.ศ.", eraBE},
		{"วันที่ 20/09/2026 ค.ศ.", eraCE},
		{"วันที่ 20/09/2026", eraAuto},
		{"", eraAuto},
	}
	for _, tc := range cases {
		if got := detectEra(tc.text); got != tc.want {
			t.Errorf("detectEra(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestNormalizeYear(t *testing.T) {
	cases := []struct {
		name string
		s    string
		hint era
		want int
	}{
		{"4-digit BE marker", "2569", eraBE, 2026},
		{"4-digit CE marker", "2026", eraCE, 2026},
		{"4-digit auto below threshold stays CE", "2026", eraAuto, 2026},
		{"4-digit auto at/above 2400 becomes BE", "2569", eraAuto, 2026},
		{"2-digit CE marker", "26", eraCE, 2026},
		{"2-digit BE marker", "69", eraBE, 2026},
		{"2-digit auto below 60 treated as CE", "26", eraAuto, 2026},
		{"2-digit auto at/above 60 treated as BE", "69", eraAuto, 2026},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeYear(tc.s, tc.hint); got != tc.want {
				t.Errorf("normalizeYear(%q, %v) = %d, want %d", tc.s, tc.hint, got, tc.want)
			}
		})
	}
}

func TestRelativeDay(t *testing.T) {
	cases := []struct {
		text    string
		wantOff int
		wantOK  bool
	}{
		{"มะรืนนี้ไปหาหมอ", 2, true},
		{"พรุ่งนี้ไปเที่ยว", 1, true},
		{"tomorrow morning", 1, true},
		{"วันนี้มีประชุม", 0, true},
		{"คืนนี้ดูหนัง", 0, true},
		{"today at noon", 0, true},
		{"tonight we party", 0, true},
		{"no relative word here", 0, false},
	}
	for _, tc := range cases {
		gotOff, gotOK := relativeDay(tc.text)
		if gotOff != tc.wantOff || gotOK != tc.wantOK {
			t.Errorf("relativeDay(%q) = (%d, %v), want (%d, %v)", tc.text, gotOff, gotOK, tc.wantOff, tc.wantOK)
		}
	}
}

func TestFindClock(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantHour   int
		wantMinute int
		wantOK     bool
	}{
		{"dot separator", "เตือน 9.30 กินยา", 9, 30, true},
		{"colon separator", "เตือน 9:30 กินยา", 9, 30, true},
		{"เวลา keyword without minutes", "เตือนเวลา 21 น. นอน", 21, 0, true},
		{"digit-then-marker without เวลา", "อ่านหนังสือ 21 น.", 21, 0, true},
		{"out-of-range hour skipped, valid one found after", "88.99 แต่ 7.15 นัด", 7, 15, true},
		{"no clock at all", "ไปเที่ยวพรุ่งนี้", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, m, ok := findClock(tc.text)
			if h != tc.wantHour || m != tc.wantMinute || ok != tc.wantOK {
				t.Errorf("findClock(%q) = (%d, %d, %v), want (%d, %d, %v)", tc.text, h, m, ok, tc.wantHour, tc.wantMinute, tc.wantOK)
			}
		})
	}
}
