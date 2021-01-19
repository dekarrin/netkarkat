package testutil

import (
	"io"
	"testing"
)

// various utility functions for testing

// GetReadSeekerPosition gets the current seeker position, and fails the test if there is an error.
// Because the ReadSeeker is moved only to the current position, this should never fail.
func GetReadSeekerPosition(r io.ReadSeeker, t *testing.T) int64 {
	pos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatalf("error getting current ReadSeeker position; this should never happen: %v", err)
	}
	return pos
}
