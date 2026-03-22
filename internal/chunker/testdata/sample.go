package sample

import "fmt"

// Buffer holds bytes in memory.
// It supports read and write operations.
type Buffer struct {
	data []byte
	pos  int
}

// NewBuffer creates a new empty Buffer.
func NewBuffer() *Buffer {
	return &Buffer{}
}

// Write appends data to the buffer.
func (b *Buffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// String returns the buffer contents as a string.
func (b *Buffer) String() string {
	return fmt.Sprintf("%s", b.data)
}
