package macros

import (
	"testing"
)

func Test_macroset_Apply(t *testing.T) {
	sut := testMacrosetWithMacros(t, map[string]string{
		"MACRO":           "<macrofill 1>",
		"SUPERMACRO":      "MACRO<with super>",
		"MACRO2":          "2",
		"SUPERMACRO_OF_2": "MACRO MACRO2",
	})

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
				t.Fatalf("expected %q but got: %q", tc.expected, actual)
			}
		})
	}
}

func Test_macroset_Rename(t *testing.T) {
	// each test case will get a new macroset with these macros defined:
	predefinedMacros := map[string]string{
		"MACRO_TO_RENAME":   "<macrofill 1>",
		"SUPERMACRO":        "MACRO_TO_RENAME<with super>MACRO_TO_RENAME",
		"SUPERMACRO_SPACES": "MACRO_TO_RENAME <with super> MACRO_TO_RENAME",
	}

	// actual test cases are to do the rename
	testCases := []struct {
		test      string
		from      string
		to        string
		replace   bool
		expectErr bool
	}{
		{test: "normal rename", from: "MACRO_TO_RENAME", to: "SOMETHING_ELSE"},
		{test: "give same name", from: "MACRO_TO_RENAME", to: "MACRO_TO_RENAME"},
		{test: "blank new name fails", from: "MACRO_TO_RENAME", to: "", expectErr: true},
		{test: "blank new name fails (unmatched case)", from: "macro_TO_RENAME", to: "", expectErr: true},
		{test: "new name conflict fails", from: "MACRO_TO_RENAME", to: "SUPERMACRO", expectErr: true},
		{test: "new name conflict fails (unmatched case)", from: "macro_TO_RENAME", to: "superMAcro", expectErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.test, func(t *testing.T) {
			sut := testMacrosetWithMacros(t, predefinedMacros)

			err := sut.Rename(tc.from, tc.to, tc.replace)

			// check for error
			if err != nil && !tc.expectErr {
				t.Fatalf("returned an error during rename: %v", err)
			} else if err == nil && tc.expectErr {
				t.Fatalf("expected an error during rename but nil error was returned")
			}

			supermacroExpected := "<macrofill 1><with super><macrofill 1>"
			supermacroSpacesExpected := "<macrofill 1> <with super> <macrofill 1>"
			if !tc.replace {
				// if replace not enabled, the rename doesn't update other macros that contain it so
				// executing macros that contain the original should now just print the old macro name
				supermacroExpected = predefinedMacros["SUPERMACRO"]
				supermacroSpacesExpected = predefinedMacros["SUPERMACRO_SPACES"]
			}

			// validate: apply against new name gives original content
			assertMacrosetApply(t, sut, tc.to, predefinedMacros["MACRO_TO_RENAME"])
			assertMacrosetApply(t, sut, "SUPERMACRO", supermacroExpected)
			assertMacrosetApply(t, sut, "SUPERMACRO_SPACES", supermacroSpacesExpected)
		})
	}
}

func assertMacrosetApply(t *testing.T, sut macroset, input string, expected string) {
	actual, err := sut.Apply(input)
	if err != nil {
		t.Fatalf("assertMacrosetApply: sut.Apply() returned an error: %v", err)
	}
	if actual != expected {
		t.Fatalf("assertMacrosetApply: expected %q but got: %q", expected, actual)
	}
}

func testMacrosetWithMacros(t *testing.T, macroDefs map[string]string) macroset {
	var sut macroset
	for name, content := range macroDefs {
		if err := sut.Define(name, content); err != nil {
			t.Fatalf("prep step: pre-test macro definition for %q failed: %v", name, err)
		}
	}
	return sut
}
