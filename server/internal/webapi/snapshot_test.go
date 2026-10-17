package webapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLargeSnapshotUsesBoundedUnicodeSafeChunks(t *testing.T) {
	text := strings.Repeat("测\"\n", 7<<20)
	value := map[string]any{"type": "snapshot", "streamId": "s", "state": map[string]any{"text": text}}
	var chunks strings.Builder
	part := 0
	ended := false
	err := writeSnapshot(value, func(frame any) error {
		b, e := json.Marshal(frame)
		if e != nil {
			t.Fatal(e)
		}
		if len(b) > 2<<20 {
			t.Fatal("unbounded frame", len(b))
		}
		var v map[string]any
		if e = json.Unmarshal(b, &v); e != nil {
			t.Fatal(e)
		}
		switch v["type"] {
		case "snapshot_start":
			if v["parts"].(float64) < 2 {
				t.Fatal(v)
			}
		case "snapshot_chunk":
			if v["index"].(float64) != float64(part) {
				t.Fatal(v)
			}
			part++
			chunks.WriteString(v["data"].(string))
		case "snapshot_end":
			ended = true
		default:
			t.Fatal("unchunked large snapshot")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ended {
		t.Fatal("no end")
	}
	var got struct{ State struct{ Text string } }
	if e := json.Unmarshal([]byte(chunks.String()), &got); e != nil {
		t.Fatal(e)
	}
	if got.State.Text != text {
		t.Fatal("snapshot changed")
	}
}
