package misc

import (
	"fmt"
	"io"
)

// ReadNBytes reads exactly n bytes from the reader. An error is returned if the number of bytes read does not match n.
func ReadNBytes(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	numRead, err := r.Read(buf)
	if err != nil {
		return nil, err
	}
	if numRead != n {
		return nil, fmt.Errorf("incorrect number of bytes read; wanted %d but got %d", n, numRead)
	}
	return buf, nil
}

// Read1Byte reads exactly 1 byte from the reader. An error is returned if the number of bytes read is not 1.
func Read1Byte(r io.Reader) (byte, error) {
	buf, err := ReadNBytes(r, 1)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

// ExamineNBytes reades exactly n bytes from the reader and then puts the position back to where it originally was.
func ExamineNBytes(r io.ReadSeeker, n int) ([]byte, error) {
	n64 := int64(n)
	if int(n64) != n || int(-n64) != -n {
		return nil, fmt.Errorf("number of bytes to examine is too large: %v", n)
	}
	if n64 < 1 {
		return nil, fmt.Errorf("number of bytes to examine is less than 1: %v", n)
	}
	readBytes, err := ReadNBytes(r, n)
	if err != nil {
		return nil, err
	}
	_, err = r.Seek(-n64, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("could seek back after reading bytes: %v", err)
	}
	return readBytes, nil
}

// Examine1Byte reades exactly 1 byte from the reader and then puts the position back to where it originally was.
func Examine1Byte(r io.ReadSeeker) (byte, error) {
	buf, err := ExamineNBytes(r, 1)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}
