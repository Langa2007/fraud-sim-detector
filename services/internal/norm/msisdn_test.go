package norm

import (
	"testing"
)

func TestNormalizeE164(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		err      bool
	}{
		{input: "+254712345678", expected: "+254712345678", err: false},
		{input: "0712345678", expected: "+254712345678", err: false},
		{input: "0112345678", expected: "+254112345678", err: false},
		{input: "254712345678", expected: "+254712345678", err: false},
		{input: "712345678", expected: "+254712345678", err: false},
		{input: "+254 712-345-678", expected: "+254712345678", err: false},
		{input: "(0712) 345 678", expected: "+254712345678", err: false},
		{input: "+14155552671", expected: "+14155552671", err: false},
		{input: "", expected: "", err: true},
		{input: "abc", expected: "", err: true},
		{input: "+0123456", expected: "", err: true},
		{input: "+254", expected: "", err: true},
	}

	for _, tc := range tests {
		got, err := NormalizeE164(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("NormalizeE164(%q): expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("NormalizeE164(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.expected {
				t.Errorf("NormalizeE164(%q) = %q; want %q", tc.input, got, tc.expected)
			}
			if !IsE164(got) {
				t.Errorf("IsE164(%q) = false; want true", got)
			}
		}
	}
}
