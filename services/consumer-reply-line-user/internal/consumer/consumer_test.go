package consumer

import (
	"reflect"
	"testing"
)

func TestSplitReplyMessages(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single message",
			text: "hello there",
			want: []string{"hello there"},
		},
		{
			name: "split on blank lines",
			text: "first part\n\nsecond part\n\nthird part",
			want: []string{"first part", "second part", "third part"},
		},
		{
			name: "ignore empty segments",
			text: "\n\nfirst part\n\n\nsecond part\n\n",
			want: []string{"first part", "second part"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitReplyMessages(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitReplyMessages(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
