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
