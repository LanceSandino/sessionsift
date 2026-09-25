// Package jsonlines provides bounded, recoverable line scanning for JSONL inputs.
// @module JSONLines keeps one oversized record from aborting an entire import.
package jsonlines

import (
	"bufio"
	"io"
)

// TranscriptRecordLimit bounds memory used for one provider transcript record.
// Large tool results and embedded media can exceed Scanner's usual limits, but
// a corrupt or hostile record must not cause unbounded allocation.
const TranscriptRecordLimit = 64 << 20

// Scanner reads lines up to a configured limit and discards only the contents
// of an oversized line. Scanning resumes at the next line.
type Scanner struct {
	reader    *bufio.Reader
	limit     int
	bytes     []byte
	oversized bool
	err       error
	done      bool
}

// NewScanner returns a recoverable JSONL scanner with a per-record byte limit.
func NewScanner(reader io.Reader, limit int) *Scanner {
	if limit <= 0 {
		limit = TranscriptRecordLimit
	}
	return &Scanner{reader: bufio.NewReaderSize(reader, 64<<10), limit: limit}
}

// Scan advances to the next line, including an oversized line whose contents
// were discarded. Call Oversized before consuming Bytes.
func (s *Scanner) Scan() bool {
	if s.done {
		return false
	}
	s.bytes = s.bytes[:0]
	s.oversized = false
	sawData := false
	for {
		chunk, err := s.reader.ReadSlice('\n')
		if len(chunk) > 0 {
			sawData = true
		}
		if err != bufio.ErrBufferFull && len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			chunk = chunk[:len(chunk)-1]
			if len(chunk) > 0 && chunk[len(chunk)-1] == '\r' {
				chunk = chunk[:len(chunk)-1]
			}
		}
		if !s.oversized {
			if len(chunk) > s.limit-len(s.bytes) {
				s.bytes = nil
				s.oversized = true
			} else {
				s.bytes = append(s.bytes, chunk...)
			}
		}
		switch err {
		case nil:
			return true
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			s.done = true
			return sawData
		default:
			s.err = err
			s.done = true
			return false
		}
	}
}

// Bytes returns the current line without its newline delimiter.
func (s *Scanner) Bytes() []byte { return s.bytes }

// Oversized reports that the current line exceeded the configured limit and
// was discarded so scanning could continue.
func (s *Scanner) Oversized() bool { return s.oversized }

// Err returns the first non-EOF read error.
func (s *Scanner) Err() error { return s.err }
