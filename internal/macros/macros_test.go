package macros

import (
	"testing"
)

func Test_macroset_Apply(t *testing.T) {
	macroDefs := [][2]string{
		{"MACRO", "<macrofill 1>"},
		{"SUPERMACRO", "MACRO<with super>"},
		{"MACRO2", "2"},
		{"SUPERMACRO_OF_2", "MACRO MACRO2"},
	}

	sut := macroset{}
	for _, def := range macroDefs {
		if err := sut.Define(def[0], def[1]); err != nil {
			t.Fatalf("prep step: pre-test macro definition for %q failed: %v", def[0], err)
		}
	}

	testCases := []struct {
		input     string
		expected  string
		expectErr bool
	}{
		{input: "MACRO", expected: "<macrofill 1>"},
		{input: " MACRO ", expected: " <macrofill 1> "},
		{input: "  MACRO  ", expected: "  <macrofill 1>  "},
		{input: "  MACRO  ", expected: "  <macrofill 1>  "},
		{input: "before MACRO", expected: "before <macrofill 1>"},
		{input: " before MACRO", expected: " before <macrofill 1>"},
		{input: "MACRO after", expected: "<macrofill 1> after"},
		{input: "MACRO after ", expected: "<macrofill 1> after "},
		{input: "MACRO after\t", expected: "<macrofill 1> after\t"},
		{input: "SUPERMACRO", expected: "<macrofill 1><with super>"},
		{input: "SUPERMACRO_OF_2", expected: "<macrofill 1> 2"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			actual, err := sut.Apply(tc.input)

			// check for error
			if err != nil && !tc.expectErr {
				t.Fatalf("returned an error: %v", err)
			} else if err == nil && tc.expectErr {
				t.Fatalf("expected an error but nil error was returned")
			}

			// check the value
			if tc.expected != actual {
				t.Errorf("expected %q but got: %q", tc.expected, actual)
			}
		})
	}
}
