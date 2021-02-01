package console

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func Test_parseLineToBytes(t *testing.T) {
	testCases := []struct {
		input     string
		expected  []byte
		expectErr bool
	}{
		{input: "", expected: []byte{}},
		{input: "hello", expected: []byte{0x68, 0x65, 0x6C, 0x6C, 0x6F}},
		{input: "\\x4f", expected: []byte{0x4f}},
		{input: "\\x4f\\x2E", expected: []byte{0x4f, 0x2e}},
		{input: "\\\\", expected: []byte{0x5c}},
		{input: "\\x4", expectErr: true},
		{input: "\\x", expectErr: true},
		{input: "\\", expectErr: true},
		{input: "\\a", expectErr: true},
	}

	for _, tc := range testCases {
		t.Run("parseLineToBytes input "+tc.input, func(t *testing.T) {

			sut := consoleState{}
			actual, err := sut.parseLineToBytes(tc.input)

			// check for error
			if err != nil && !tc.expectErr {
				t.Fatalf("returned an error: %v", err)
			} else if err == nil && tc.expectErr {
				t.Fatalf("expected an error but nil error was returned")
			}

			// check the value
			if bytes.Compare(tc.expected, actual) != 0 {
				t.Errorf("expected %s but got: %s", hex.EncodeToString(tc.expected), hex.EncodeToString(actual))
			}
		})
	}
}
