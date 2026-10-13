package pirpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

type jsonlReader struct {
	reader *bufio.Reader
	max    int
}

func newJSONLReader(r io.Reader, maxBytes int) *jsonlReader {
	return &jsonlReader{reader: bufio.NewReaderSize(r, 4096), max: maxBytes}
}
func (r *jsonlReader) Next() (json.RawMessage, error) {
	var b []byte
	for {
		c, err := r.reader.ReadByte()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, newError("io", NotSent, err)
			}
			if len(b) == 0 {
				return nil, io.EOF
			}
			if len(b) > r.max {
				return nil, ErrRecordTooLarge
			}
			if !jsonObject(b) {
				return nil, ErrProtocol
			}
			return b, nil
		}
		if c == '\n' {
			if len(b) > 0 && b[len(b)-1] == '\r' {
				b = b[:len(b)-1]
			}
			if len(b) > r.max {
				return nil, ErrRecordTooLarge
			}
			if !jsonObject(b) {
				return nil, ErrProtocol
			}
			return b, nil
		}
		// One extra CR can be a CRLF delimiter, never an unbounded tail.
		if len(b) > r.max || len(b) == r.max && c != '\r' {
			return nil, ErrRecordTooLarge
		}
		b = append(b, c)
	}
}
