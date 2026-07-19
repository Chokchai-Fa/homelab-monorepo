// Package flex builds the reminder notification's flex bubble as typed Go
// structs marshaled with encoding/json, so user-supplied text is always
// properly escaped - never built by string concatenation.
package flex

import (
	"encoding/json"
	"time"
)

var bangkok = time.FixedZone("ICT", 7*60*60)

type box struct {
	Type            string `json:"type"`
	Layout          string `json:"layout"`
	Contents        []any  `json:"contents"`
	BackgroundColor string `json:"backgroundColor,omitempty"`
	Spacing         string `json:"spacing,omitempty"`
	Margin          string `json:"margin,omitempty"`
}

type text struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Weight string `json:"weight,omitempty"`
	Color  string `json:"color,omitempty"`
	Size   string `json:"size,omitempty"`
	Wrap   bool   `json:"wrap,omitempty"`
}

type separator struct {
	Type string `json:"type"`
}

type bubble struct {
	Type   string `json:"type"`
	Header box    `json:"header"`
	Body   box    `json:"body"`
}

// Build renders the reminder as a flex bubble: a colored header, the
// reminder message, and a footer with who set it and when. displayName may
// be empty (falls back to "someone").
func Build(message, displayName string, remindAt time.Time) (json.RawMessage, error) {
	if displayName == "" {
		displayName = "someone"
	}
	b := bubble{
		Type: "bubble",
		Header: box{
			Type:            "box",
			Layout:          "vertical",
			BackgroundColor: "#7D5FFF",
			Contents: []any{
				text{Type: "text", Text: "⏰ แจ้งเตือน", Weight: "bold", Color: "#FFFFFF"},
			},
		},
		Body: box{
			Type:    "box",
			Layout:  "vertical",
			Spacing: "md",
			Contents: []any{
				text{Type: "text", Text: message, Wrap: true, Size: "lg"},
				separator{Type: "separator"},
				box{
					Type:    "box",
					Layout:  "vertical",
					Spacing: "xs",
					Contents: []any{
						text{Type: "text", Text: "จาก: " + displayName, Size: "sm", Color: "#888888"},
						text{Type: "text", Text: "เวลา: " + remindAt.In(bangkok).Format("02/01/2006 15:04"), Size: "sm", Color: "#888888"},
					},
				},
			},
		},
	}
	return json.Marshal(b)
}
