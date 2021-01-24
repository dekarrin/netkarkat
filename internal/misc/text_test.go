package misc

import (
	"testing"
)

func Test_CollapseWhitespace(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty input", input: "", expected: ""},
		{name: "no spaces", input: "hello", expected: "hello"},
		{name: "single space at middle", input: "hello there", expected: "hello there"},
		{name: "single space at end", input: "hello ", expected: "hello "},
		{name: "single space at start", input: " hello", expected: " hello"},
		{name: "two spaces at middle", input: "hello  there", expected: "hello there"},
		{name: "two spaces at end", input: "hello  ", expected: "hello "},
		{name: "two spaces at start", input: "  hello", expected: " hello"},
		{name: "many spaces at middle", input: "hello         there", expected: "hello there"},
		{name: "many spaces at end", input: "hello       ", expected: "hello "},
		{name: "many spaces at start", input: "              hello", expected: " hello"},
		{name: "many mixed latin-1 whitespace chars at middle", input: "hello \t\v\r\nthere", expected: "hello there"},
		{name: "many mixed extended whitespace chars at middle", input: "hello\u3000\u205f\u202fthere", expected: "hello there"},
		{name: "many mixed latin-1 and extended whitespace chars at middle", input: "hello\u2003\u2007 \nthere", expected: "hello there"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			actual := CollapseWhitespace(tc.input)

			if actual != tc.expected {
				t.Fatalf("expected %q but got %q", tc.expected, actual)
			}
		})
	}
}
