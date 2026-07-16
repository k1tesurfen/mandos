package main

import "testing"

func TestAppleQuote(t *testing.T) {
	cases := map[string]string{
		`hi`:        `"hi"`,
		`Passwort:`: `"Passwort:"`,
		`a"b`:       `"a\"b"`, // embedded double-quote is escaped
		`a\b`:       `"a\\b"`, // embedded backslash is escaped
		``:          `""`,
	}
	for in, want := range cases {
		if got := appleQuote(in); got != want {
			t.Errorf("appleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
