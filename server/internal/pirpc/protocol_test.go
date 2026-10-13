package pirpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCommandEnvelope(t *testing.T) {
	b, err := encodeCommand("req_1", Command{Type: "get_state"}, 1024)
	if err != nil || !bytes.HasSuffix(b, []byte("\n")) {
		t.Fatalf("%q %v", b, err)
	}
	var v map[string]json.RawMessage
	if err = json.Unmarshal(b, &v); err != nil || len(v) != 2 || string(v["id"]) != `"req_1"` || string(v["type"]) != `"get_state"` {
		t.Fatalf("%s %v", b, err)
	}
	for _, typ := range []string{"", "UPPER", "x-y", strings.Repeat("a", 65), "extension_ui_response"} {
		if _, err = encodeCommand("r", Command{Type: typ}, 1024); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("type %q: %v", typ, err)
		}
	}
	for _, p := range []string{`null`, `[]`, `1`, `{"id":"bad"}`, `{"t\u0079pe":"bad"}`, `{"x":`, "{\"x\":\"\xff\"}"} {
		if _, err = encodeCommand("r", Command{Type: "prompt", Params: json.RawMessage(p)}, 1024); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("params %q: %v", p, err)
		}
	}
	b, err = encodeCommand("r", Command{Type: "prompt", Params: json.RawMessage(`{"message":"你好","unknown":7}`)}, 1024)
	if err != nil || !json.Valid(b) {
		t.Fatalf("%q %v", b, err)
	}
	if _, err = encodeCommand("r", Command{Type: "get_state"}, 2); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatal(err)
	}
}
func TestDecodeResponse(t *testing.T) {
	for _, raw := range []string{`{"type":"response","id":"r","command":"abort","success":true}`, `{"type":"response","id":"r","command":"prompt","success":false,"error":"rejected"}`} {
		r, err := decodeRecord([]byte(raw))
		if err != nil || r.Response == nil || r.Event != nil || r.Response.ID != "r" || string(r.Response.Raw) != raw {
			t.Fatalf("%+v %v", r, err)
		}
	}
	for _, raw := range []string{`{"type":"response","id":"r","command":"abort"}`, `{"type":"response","id":"r","command":"abort","success":null}`, `{"type":"response","id":"r","command":"abort","success":false}`, `{"type":"response","command":"parse","success":false,"error":"bad"}`, `{"type":"response","id":"","command":"x","success":true}`, `{"type":"response","id":1,"command":"x","success":true}`} {
		if _, err := decodeRecord([]byte(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}
func TestDecodeUnknownEvent(t *testing.T) {
	for _, raw := range []string{`{"type":"future_event","extra":{"x":1}}`, `{"type":"extension_ui_request","id":"req_1","method":"confirm"}`} {
		r, err := decodeRecord([]byte(raw))
		if err != nil || r.Event == nil || r.Response != nil || string(r.Event.Raw) != raw {
			t.Fatalf("%+v %v", r, err)
		}
	}
	for _, raw := range []string{`{}`, `{"type":""}`, `{"type":null}`, `{"type":2}`, `[]`, `null`, `bad`} {
		if _, err := decodeRecord([]byte(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}
func TestValidateState(t *testing.T) {
	good := `{"sessionId":"fixture-session","isStreaming":false,"isCompacting":false,"pendingMessageCount":0,"future":true}`
	if err := validateState(json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`null`, `{}`, strings.Replace(good, `"fixture-session"`, `""`, 1), strings.Replace(good, `"isStreaming":false`, `"isStreaming":0`, 1), strings.Replace(good, `"isCompacting":false`, `"isCompacting":null`, 1), strings.Replace(good, `"pendingMessageCount":0`, `"pendingMessageCount":-1`, 1), strings.Replace(good, `"pendingMessageCount":0`, `"pendingMessageCount":0.5`, 1)} {
		if err := validateState(json.RawMessage(bad)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}
func TestErrorPrivacy(t *testing.T) {
	secret := "synthetic-secret"
	e := &Error{Code: "remote_rejected", Delivery: Acknowledged, cause: errors.New(secret)}
	if strings.Contains(e.Error(), secret) || !errors.Is(e, ErrRemoteRejected) || e.Unwrap().Error() != secret {
		t.Fatal("error contract")
	}
	ce := &Error{Code: "deadline", cause: context.DeadlineExceeded}
	if !errors.Is(ce, context.DeadlineExceeded) || !errors.Is(ce, ErrDeadline) {
		t.Fatal(ce)
	}
	var target *Error
	if !errors.As(e, &target) || target.Delivery != Acknowledged {
		t.Fatal("delivery")
	}
}
