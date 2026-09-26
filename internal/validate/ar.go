// Package validate provides .deb file validation including ar archive and control file parsing.
package validate

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ArHeader represents an ar archive member header
type ArHeader struct {
	Name    string
	ModTime int64
	UID     int
	GID     int
	Mode    int
	Size    int64
}

// ArReader reads ar archive format (used in .deb files)
type ArReader struct {
	r      io.Reader
	buf    [8]byte
	member *memberReader
}

type memberReader struct {
	r        io.Reader
	size     int64
	consumed int64
}

// NewArReader creates a new ar reader
func NewArReader(r io.Reader) *ArReader {
	return &ArReader{r: r}
}

// Next reads the next ar member header
func (ar *ArReader) Next() (*ArHeader, error) {
	// Skip any padding after previous member (ar members are 2-byte aligned)
	if ar.member != nil {
		if ar.member.consumed%2 != 0 {
			if _, err := io.ReadFull(ar.r, ar.buf[:1]); err != nil && err != io.EOF {
				return nil, err
			}
		}
	}

	// Read magic number on first call
	if ar.member == nil {
		if _, err := io.ReadFull(ar.r, ar.buf[:8]); err != nil {
			return nil, err
		}
		if string(ar.buf[:]) != "!<arch>\n" {
			return nil, fmt.Errorf("invalid ar archive magic")
		}
	}

	// Read 60-byte header
	header := make([]byte, 60)
	if _, err := io.ReadFull(ar.r, header); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read ar member header: %w", err)
	}

	// Parse header fields (all are ASCII)
	arHeader := &ArHeader{
		Name:    strings.TrimRight(string(header[0:16]), " /"),
		ModTime: parseArField(string(header[16:28])),
		UID:     int(parseArField(string(header[28:34]))),
		GID:     int(parseArField(string(header[34:40]))),
		Mode:    int(parseArField(string(header[40:48]))),
		Size:    parseArField(string(header[48:58])),
	}

	// Verify trailer
	if string(header[58:60]) != "`\n" {
		return nil, fmt.Errorf("invalid ar member header trailer")
	}

	ar.member = &memberReader{
		r:        ar.r,
		size:     arHeader.Size,
		consumed: 0,
	}

	return arHeader, nil
}

// Read reads from the current ar member
func (ar *ArReader) Read(p []byte) (int, error) {
	if ar.member == nil {
		return 0, io.EOF
	}
	return ar.member.Read(p)
}

// Read from memberReader
func (mr *memberReader) Read(p []byte) (int, error) {
	if mr.consumed >= mr.size {
		return 0, io.EOF
	}

	toRead := int64(len(p))
	if toRead > mr.size-mr.consumed {
		toRead = mr.size - mr.consumed
	}

	n, err := mr.r.Read(p[:toRead])
	mr.consumed += int64(n)
	return n, err
}

// parseArField parses a whitespace-padded octal or decimal field
func parseArField(s string) int64 {
	s = strings.TrimSpace(s)
	val, _ := strconv.ParseInt(s, 10, 64)
	return val
}
