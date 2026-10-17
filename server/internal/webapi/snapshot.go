package webapi

import (
	"encoding/json"
	"unicode/utf8"
)

// Large authority snapshots are split independently of the replay window. The
// receiver applies nothing until every ordered part has arrived on this socket.
func writeSnapshot(value any, write func(any) error) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	const size = 256 << 10
	if len(body) <= size {
		return write(value)
	}
	// JSON is UTF-8. Never split inside a rune when embedding chunks as strings.
	chunks := make([]string, 0, (len(body)+size-1)/size)
	for len(body) > 0 {
		n := min(size, len(body))
		for n < len(body) && !utf8.RuneStart(body[n]) {
			n--
		}
		chunks = append(chunks, string(body[:n]))
		body = body[n:]
	}
	if err = write(map[string]any{"type": "snapshot_start", "parts": len(chunks)}); err != nil {
		return err
	}
	for i, chunk := range chunks {
		if err = write(map[string]any{"type": "snapshot_chunk", "index": i, "data": chunk}); err != nil {
			return err
		}
	}
	return write(map[string]any{"type": "snapshot_end"})
}
