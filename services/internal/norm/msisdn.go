package norm

import (
	"errors"
	"strings"
	"unicode"
)

var (
	ErrEmptyMSISDN   = errors.New("msisdn cannot be empty")
	ErrInvalidMSISDN = errors.New("invalid msisdn format")
)

// NormalizeE164 normalizes telephone numbers into standard E.164 format (+[country][national]).
// It supports Kenyan mobile formats (07xx, 01xx, 2547xx, +2547xx) and general international E.164 numbers.
func NormalizeE164(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrEmptyMSISDN
	}

	// Remove common punctuation: spaces, dashes, parentheses
	var b strings.Builder
	for i, r := range s {
		if i == 0 && r == '+' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == '-' || r == ' ' || r == '(' || r == ')' || r == '.' {
			continue
		} else {
			return "", ErrInvalidMSISDN
		}
	}
	clean := b.String()
	if clean == "" || clean == "+" {
		return "", ErrInvalidMSISDN
	}

	// If already starts with '+', validate length and digits
	if strings.HasPrefix(clean, "+") {
		digits := clean[1:]
		if len(digits) < 7 || len(digits) > 15 || digits[0] == '0' {
			return "", ErrInvalidMSISDN
		}
		return clean, nil
	}

	// Kenyan local formats:
	// 07XXXXXXXX (9 digits after 0) -> +2547XXXXXXXX
	// 01XXXXXXXX (9 digits after 0) -> +2541XXXXXXXX
	if (strings.HasPrefix(clean, "07") || strings.HasPrefix(clean, "01")) && len(clean) == 10 {
		return "+254" + clean[1:], nil
	}

	// 2547XXXXXXXX or 2541XXXXXXXX (12 digits) -> +254...
	if (strings.HasPrefix(clean, "2547") || strings.HasPrefix(clean, "2541")) && len(clean) == 12 {
		return "+" + clean, nil
	}

	// 7XXXXXXXX or 1XXXXXXXX (9 digits) -> +2547... or +2541...
	if (strings.HasPrefix(clean, "7") || strings.HasPrefix(clean, "1")) && len(clean) == 9 {
		return "+254" + clean, nil
	}

	// Generic fallback if national prefix is absent but valid length for E.164 without leading 0
	if len(clean) >= 8 && len(clean) <= 15 && clean[0] != '0' {
		return "+" + clean, nil
	}

	return "", ErrInvalidMSISDN
}

// IsE164 checks whether a string is already a valid E.164 phone number.
func IsE164(s string) bool {
	if len(s) < 8 || len(s) > 16 || s[0] != '+' || s[1] == '0' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
