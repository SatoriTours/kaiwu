package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

func TestRunInvalidConfig(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), func(string) (string, bool) { return "", true }, &stderr)
	if code != 1 {
		t.Fatalf("exit=%d want=1", code)
	}
	var entry map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["level"] != "ERROR" || entry["msg"] != "invalid configuration" {
		t.Fatalf("log=%v", entry)
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if code := run(ctx, func(string) (string, bool) { return "", false }, &stderr); code != 0 {
		t.Fatalf("exit=%d want=0", code)
	}
	if strings.Contains(stderr.String(), "server started") {
		t.Fatal("canceled command started listener")
	}
}

func TestRunPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var stderr bytes.Buffer
	code := run(context.Background(), func(key string) (string, bool) {
		if key == "KAIWU_LISTEN_ADDR" {
			return ln.Addr().String(), true
		}
		return "", false
	}, &stderr)
	if code != 1 || strings.Contains(stderr.String(), "server started") {
		t.Fatalf("exit=%d log=%s", code, stderr.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["level"] != "ERROR" || entry["msg"] != "server failed" {
		t.Fatalf("log=%v", entry)
	}
}
