package extract

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	wantTime := time.Date(2026, 7, 20, 9, 0, 0, 0, time.FixedZone("ICT", 7*3600))

	cases := []struct {
		name    string
		raw     string
		wantMsg string
		wantAt  time.Time
		wantErr bool
	}{
		{
			name:    "plain JSON",
			raw:     `{"message": "กินยา", "remind_at": "2026-07-20T09:00:00+07:00"}`,
			wantMsg: "กินยา",
			wantAt:  wantTime,
		},
		{
			name:    "fenced JSON",
			raw:     "```json\n{\"message\": \"กินยา\", \"remind_at\": \"2026-07-20T09:00:00+07:00\"}\n```",
			wantMsg: "กินยา",
			wantAt:  wantTime,
		},
		{
			name:    "prose around JSON",
			raw:     `Here you go: {"message": "call mom", "remind_at": null} hope that helps`,
			wantMsg: "call mom",
		},
		{
			name:    "both null",
			raw:     `{"message": null, "remind_at": null}`,
			wantMsg: "",
		},
		{
			name:    "not JSON",
			raw:     `sorry I cannot help with that`,
			wantErr: true,
		},
		{
			name:    "bad timestamp",
			raw:     `{"message": "x", "remind_at": "tomorrow 9am"}`,
			wantErr: true,
		},
		{
			// Multi-line messages must survive extraction verbatim.
			name:    "multi-line message",
			raw:     `{"message": "ซื้อของ:\n- นม\n- ไข่", "remind_at": "2026-07-20T09:00:00+07:00"}`,
			wantMsg: "ซื้อของ:\n- นม\n- ไข่",
			wantAt:  wantTime,
		},
		{
			// A "Z" offset is a model mistake: the instruction only ever
			// talks about Bangkok wall-clock time, so reinterpret it as +07.
			name:    "UTC Z reinterpreted as Bangkok",
			raw:     `{"message": "กินยา", "remind_at": "2026-07-20T09:00:00Z"}`,
			wantMsg: "กินยา",
			wantAt:  wantTime,
		},
		{
			name:    "missing offset treated as Bangkok",
			raw:     `{"message": "กินยา", "remind_at": "2026-07-20T09:00:00"}`,
			wantMsg: "กินยา",
			wantAt:  wantTime,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parse(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parse(%q) expected error, got %+v", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q) error: %v", tc.raw, err)
			}
			if got.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", got.Message, tc.wantMsg)
			}
			if !got.RemindAt.Equal(tc.wantAt) {
				t.Errorf("remind_at = %v, want %v", got.RemindAt, tc.wantAt)
			}
		})
	}
}

func TestRefine(t *testing.T) {
	at := func(y int, mo time.Month, d, h, mi int) time.Time {
		return time.Date(y, mo, d, h, mi, 0, 0, bangkok)
	}
	noon := at(2026, 7, 19, 12, 0)          // "now" for most cases
	nearMidnight := at(2026, 7, 19, 23, 50) // "now" for the midnight cases

	cases := []struct {
		name  string
		text  string
		llmAt time.Time
		now   time.Time
		want  time.Time
	}{
		{
			name:  "explicit date and dot time override the LLM",
			text:  "วันที่ 20/09/2026 เวลา 00.25 ไปรับแม่",
			llmAt: at(2026, 9, 21, 0, 25), // LLM got the day wrong
			now:   noon,
			want:  at(2026, 9, 20, 0, 25),
		},
		{
			name: "buddhist era year",
			text: "20/09/2569 เวลา 18.30 น. นัดหมอ",
			now:  noon,
			want: at(2026, 9, 20, 18, 30),
		},
		{
			name: "two-digit BE year",
			text: "20/09/69 9.00 ประชุม",
			now:  noon,
			want: at(2026, 9, 20, 9, 0),
		},
		{
			name: "two-digit CE year",
			text: "20/09/26 9.00 ประชุม",
			now:  noon,
			want: at(2026, 9, 20, 9, 0),
		},
		{
			name: "explicit BE marker wins over magnitude",
			text: "วันที่ 20/09/2569 พ.ศ. เวลา 10.00 นัดหมอ",
			now:  noon,
			want: at(2026, 9, 20, 10, 0),
		},
		{
			name: "explicit CE marker on a two-digit year",
			text: "20/09/26 ค.ศ. เวลา 10.00 นัดหมอ",
			now:  noon,
			want: at(2026, 9, 20, 10, 0),
		},
		{
			name: "bare time later today stays today",
			text: "เตือน 23.45 ปิดแก๊ส",
			now:  noon,
			want: at(2026, 7, 19, 23, 45),
		},
		{
			name:  "just-after-midnight time rolls to the next occurrence",
			text:  "เตือน 0.25 กินยา",
			llmAt: at(2026, 7, 19, 0, 25), // LLM resolved to earlier today
			now:   nearMidnight,
			want:  at(2026, 7, 20, 0, 25),
		},
		{
			name:  "relative day pins the date for an explicit clock",
			text:  "พรุ่งนี้ 9.30 ไปธนาคาร",
			llmAt: at(2026, 7, 19, 9, 30), // LLM dropped the "tomorrow"
			now:   noon,
			want:  at(2026, 7, 20, 9, 30),
		},
		{
			name:  "explicit date keeps the LLM's clock",
			text:  "วันที่ 20/09/2026 ตอนเย็น จ่ายค่าน้ำ",
			llmAt: at(2026, 7, 19, 18, 0),
			now:   noon,
			want:  at(2026, 9, 20, 18, 0),
		},
		{
			name: "hour with clock marker",
			text: "เตือนเวลา 21 น. อ่านหนังสือ",
			now:  noon,
			want: at(2026, 7, 19, 21, 0),
		},
		{
			name:  "nothing explicit trusts the LLM",
			text:  "พรุ่งนี้เช้าๆ ไปวิ่ง",
			llmAt: at(2026, 7, 20, 7, 0),
			now:   noon,
			want:  at(2026, 7, 20, 7, 0),
		},
		{
			name: "explicit date without any clock defers to the flow",
			text: "วันที่ 20/09/2026 ไปหาหมอ",
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
