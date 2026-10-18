package pirpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func capturedResponses(t *testing.T, path string) []map[string]any {
	t.Helper()
	return capturedResponsesWaiting(t, path, 0)
}

func capturedResponsesWaiting(t *testing.T, path string, want int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		records := readCaptured(t, path)
		if len(records) >= want || time.Now().After(deadline) {
			return records
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readCaptured(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("interleaved or invalid record %q: %v", scanner.Text(), err)
		}
		out = append(out, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func startUIHelper(t *testing.T) (*Client, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "ui.jsonl")
	launch := helperLaunch(t, "rpc_ui")
	launch.Env = append(launch.Env, "KAIWU_PI_UI_CAPTURE="+capture)
	client, err := Start(context.Background(), launch, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = client.Close(ctx)
	})
	return client, capture
}

func TestRespondUIRoutesRawRecordWithOriginalID(t *testing.T) {
	client, capture := startUIHelper(t)
	event, err := client.NextEvent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "extension_ui_request" || !strings.Contains(string(event.Raw), "upstream-ui-1") {
		t.Fatalf("unexpected event %+v", event)
	}
	confirmed := false
	delivery, err := client.RespondUI(context.Background(), UIResponse{ID: "upstream-ui-1", Confirmed: &confirmed})
	if err != nil || delivery != Acknowledged {
		t.Fatalf("respond confirm=false: %v %v", delivery, err)
	}
	value := "Allow"
	if delivery, err = client.RespondUI(context.Background(), UIResponse{ID: "upstream-ui-notify", Value: &value}); err != nil || delivery != Acknowledged {
		t.Fatalf("respond value: %v %v", delivery, err)
	}
	records := capturedResponsesWaiting(t, capture, 2)
	if len(records) != 2 {
		t.Fatalf("captured %d records: %v", len(records), records)
	}
	first := records[0]
	if first["type"] != "extension_ui_response" || first["id"] != "upstream-ui-1" {
		t.Fatalf("raw record %v", first)
	}
	// false must survive as an explicit boolean, never be dropped.
	if got, ok := first["confirmed"].(bool); !ok || got != false {
		t.Fatalf("false not preserved: %v", first)
	}
	if _, has := first["value"]; has {
		t.Fatal("confirm record carries a value")
	}
	if records[1]["value"] != "Allow" || records[1]["id"] != "upstream-ui-notify" {
		t.Fatalf("value record %v", records[1])
	}
	// No req_N waiter may have been registered for the raw family.
	if stats := client.Stats(); stats.IgnoredResponses != 0 {
		t.Fatalf("unexpected response accounting %+v", stats)
	}
}

func TestRespondUIRejectsAmbiguousOrOversizeShape(t *testing.T) {
	client, capture := startUIHelper(t)
	value := "x"
	yes := true
	for name, response := range map[string]UIResponse{
		"no-discriminator":  {ID: "upstream-ui-1"},
		"two-fields":        {ID: "upstream-ui-1", Value: &value, Confirmed: &yes},
		"cancel-plus-value": {ID: "upstream-ui-1", Value: &value, Cancelled: true},
		"bad-id":            {ID: "upstream/ui", Value: &value},
		"empty-id":          {ID: "", Value: &value},
		"empty-utf8":        {ID: "upstream-ui-1", Value: stringPtrRaw("\xff")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := client.RespondUI(context.Background(), response); err == nil {
				t.Fatal("invalid raw response accepted")
			}
		})
	}
	huge := strings.Repeat("x", maxUIResponseBytes)
	if _, err := client.RespondUI(context.Background(), UIResponse{ID: "upstream-ui-1", Value: &huge}); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("oversize accepted: %v", err)
	}
	if records := capturedResponses(t, capture); len(records) != 0 {
		t.Fatalf("invalid responses were written: %v", records)
	}
}

func stringPtrRaw(s string) *string { return &s }

func TestRespondUISharesSingleWriterWithCommands(t *testing.T) {
	client, capture := startUIHelper(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			params, _ := json.Marshal(map[string]string{"message": "concurrent"})
			if _, err := client.Call(context.Background(), Command{Type: "prompt", Params: params}); err != nil {
				errs <- err
			}
			text := "value"
			if _, err := client.RespondUI(context.Background(), UIResponse{ID: fmt.Sprintf("upstream-ui-%d", i), Value: &text}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	records := capturedResponsesWaiting(t, capture, 4)
	if len(records) != 4 {
		t.Fatalf("expected 4 raw responses, got %d", len(records))
	}
	seen := map[string]bool{}
	for _, record := range records {
		if record["type"] != "extension_ui_response" {
			t.Fatalf("interleaved record %v", record)
		}
		seen[fmt.Sprint(record["id"])] = true
	}
	if len(seen) != 4 {
		t.Fatalf("raw responses not distinct: %v", seen)
	}
}

func TestRespondUIFailsClosedWhenProcessIsGone(t *testing.T) {
	client, err := Start(context.Background(), helperLaunch(t, "rpc_early_exit"), Options{})
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = client.Close(ctx)
		cancel()
	}
	value := "x"
	if client == nil {
		// A failed start with confirmed cleanup returns no handle at all.
		return
	}
	delivery, respondErr := client.RespondUI(context.Background(), UIResponse{ID: "upstream-ui-1", Value: &value})
	if respondErr == nil || delivery == Acknowledged {
		t.Fatalf("stopping client accepted a raw response: %v %v", delivery, respondErr)
	}
}

func TestRespondUIEmptyStringValueIsWritten(t *testing.T) {
	client, capture := startUIHelper(t)
	empty := ""
	if delivery, err := client.RespondUI(context.Background(), UIResponse{ID: "upstream-ui-1", Value: &empty}); err != nil || delivery != Acknowledged {
		t.Fatalf("empty value: %v %v", delivery, err)
	}
	records := capturedResponsesWaiting(t, capture, 1)
	if len(records) != 1 {
		t.Fatalf("captured %d", len(records))
	}
	value, ok := records[0]["value"].(string)
	if !ok || value != "" {
		t.Fatalf("empty string not preserved: %v", records[0])
	}
}
