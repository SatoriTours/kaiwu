package pirpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func helperLaunch(t *testing.T, scenario string) LaunchSpec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return LaunchSpec{Program: exe, Args: []string{"-test.run=^TestPiRPCHelperProcess$"}, Dir: t.TempDir(), Env: []string{"KAIWU_PI_RPC_HELPER=1", "KAIWU_PI_SCENARIO=" + scenario, "GORACE=atexit_sleep_ms=0"}}
}
func TestPiRPCHelperProcess(t *testing.T) {
	if os.Getenv("KAIWU_PI_RPC_HELPER") != "1" {
		t.Skip("child helper only")
	}
	if path := os.Getenv("KAIWU_PI_MARKER"); path != "" {
		_ = os.WriteFile(path, []byte("started"), 0600)
	}
	switch scenario := os.Getenv("KAIWU_PI_SCENARIO"); scenario {
	case "eof_exit":
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "ignore_eof":
		time.Sleep(time.Hour)
	case "last_output":
		_, _ = io.WriteString(os.Stdout, "last-out")
		_, _ = io.WriteString(os.Stderr, "last-err")
	case "exit_code":
		os.Exit(7)
	case "env_dir":
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"cwd": cwd, "secret": os.Getenv("KAIWU_TEST_SECRET")})
	default:
		if strings.HasPrefix(scenario, "rpc") {
			runRPCHelper(scenario)
		} else {
			os.Exit(9)
		}
	}
	os.Exit(0)
}

const helperState = `{"sessionId":"fixture-session","isStreaming":false,"isCompacting":false,"pendingMessageCount":0,"thinkingLevel":"medium","steeringMode":"all","followUpMode":"one-at-a-time","autoCompactionEnabled":false,"messageCount":0}`

func runRPCHelper(scenario string) {
	if scenario == "rpc_early_exit" {
		return
	}
	if scenario == "rpc_stdin_closed" {
		_ = os.Stdin.Close()
		time.Sleep(time.Hour)
		return
	}
	reader := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var held []map[string]any
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				switch scenario {
				case "rpc_shutdown_bad":
					_, _ = io.WriteString(os.Stdout, "not-json\n")
				case "rpc_shutdown_overflow":
					for i := 0; i < 3; i++ {
						_ = encoder.Encode(map[string]any{"type": "fixture_event"})
					}
				}
			}
			return
		}
		var cmd struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Message string `json:"message"`
			Seq     int    `json:"seq"`
		}
		if json.Unmarshal([]byte(line), &cmd) != nil {
			return
		}
		// Raw extension UI answers are a separate protocol family: pi emits no
		// normal response, so the helper must not emit one either.
		if cmd.Type == "extension_ui_response" {
			if path := os.Getenv("KAIWU_PI_UI_CAPTURE"); path != "" {
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					os.Exit(10)
				}
				_, _ = f.WriteString(line)
				_ = f.Close()
			}
			continue
		}
		r := map[string]any{"type": "response", "id": cmd.ID, "command": cmd.Type, "success": true}
		switch cmd.Type {
		case "get_state":
			if scenario == "rpc_slow_state" {
				timer := time.NewTimer(120 * time.Millisecond)
				<-timer.C
				timer.Stop()
			}
			switch scenario {
			case "rpc_no_response":
				continue
			case "rpc_bad_state":
				r["data"] = json.RawMessage(`{}`)
			case "rpc_state_type":
				r["data"] = json.RawMessage(`{"sessionId":7,"isStreaming":false,"isCompacting":false,"pendingMessageCount":0}`)
			case "rpc_state_negative":
				r["data"] = json.RawMessage(`{"sessionId":"fixture","isStreaming":false,"isCompacting":false,"pendingMessageCount":-1}`)
			case "rpc_state_fraction":
				r["data"] = json.RawMessage(`{"sessionId":"fixture","isStreaming":false,"isCompacting":false,"pendingMessageCount":0.5}`)
			case "rpc_reject_state":
				r["success"] = false
				r["error"] = "synthetic-rejected"
			default:
				r["data"] = json.RawMessage(helperState)
			}
			if scenario == "rpc_event" {
				_ = encoder.Encode(map[string]any{"type": "future_event", "x": 7})
			}
		case "fixture_stderr":
			for i := 0; i < 256; i++ {
				_, _ = io.WriteString(os.Stderr, strings.Repeat("x", 512))
			}
			_, _ = io.WriteString(os.Stderr, "synthetic-secret")
		case "fixture_stderr_close":
			_ = os.Stderr.Close()
		case "fixture_events":
			for i := 0; i < 6; i++ {
				_ = encoder.Encode(map[string]any{"type": "fixture_event", "text": strings.Repeat("x", 128)})
			}
		case "fixture_bad_json":
			_, _ = io.WriteString(os.Stdout, "not-json\n")
			continue
		case "fixture_bad_utf8":
			_, _ = os.Stdout.Write([]byte{0xff, '\n'})
			continue
		case "fixture_truncated":
			_, _ = io.WriteString(os.Stdout, `{"type":`)
			return
		case "fixture_large_record":
			_ = encoder.Encode(map[string]any{"type": "fixture_event", "text": strings.Repeat("x", 1024)})
			continue
		case "fixture_stdout_eof":
			_ = os.Stdout.Close()
			time.Sleep(time.Hour)
			return
		case "fixture_stdin_eof":
			_ = encoder.Encode(r)
			_ = os.Stdin.Close()
			time.Sleep(time.Hour)
			return
		case "fixture_last0", "fixture_last7":
			_ = encoder.Encode(map[string]any{"type": "fixture_final", "text": strings.Repeat("x", 1<<20)})
			b, _ := json.Marshal(r)
			if cmd.Message != "no_lf" {
				b = append(b, '\n')
			}
			_, _ = os.Stdout.Write(b)
			if cmd.Type == "fixture_last7" {
				os.Exit(7)
			}
			return
		case "fixture_ui":
			_ = encoder.Encode(map[string]any{"type": "extension_ui_request", "id": cmd.ID, "method": "confirm"})
		case "fixture_order":
			r["data"] = map[string]int{"seq": cmd.Seq}
			held = append(held, r)
			if len(held) == 32 {
				for i := len(held) - 1; i >= 0; i-- {
					_ = encoder.Encode(map[string]any{"type": "future_event", "seq": i})
					_ = encoder.Encode(held[i])
				}
				held = nil
			}
			continue
		case "fixture_hold":
			_ = encoder.Encode(map[string]any{"type": "fixture_received"})
			continue
		case "fixture_reject":
			r["success"] = false
			r["error"] = "synthetic-secret"
		case "fixture_late":
			_ = encoder.Encode(map[string]any{"type": "fixture_received"})
			time.Sleep(100 * time.Millisecond)
		case "fixture_duplicates":
			unknown := map[string]any{"type": "response", "id": "external", "command": cmd.Type, "success": true}
			_ = encoder.Encode(unknown)
			_ = encoder.Encode(r)
			_ = encoder.Encode(r)
			_ = encoder.Encode(map[string]any{"type": "fixture_done"})
			continue
		case "fixture_bad_command":
			r["command"] = "wrong"
		case "fixture_no_id":
			delete(r, "id")
		case "fixture_no_success":
			delete(r, "success")
		case "fixture_agent_events":
			_ = encoder.Encode(map[string]any{"type": "agent_end"})
			_ = encoder.Encode(map[string]any{"type": "agent_settled"})
		case "prompt":
			r["data"] = map[string]string{"disposition": cmd.Message}
		default:
			r["data"] = map[string]string{"disposition": "started"}
		}
		_ = encoder.Encode(r)
		if scenario == "rpc_ui" && cmd.Type == "get_state" {
			_ = encoder.Encode(map[string]any{"type": "extension_ui_request", "id": "upstream-ui-1", "method": "confirm", "title": "Clear session?", "message": "All messages will be lost.", "timeout": 5000})
			_ = encoder.Encode(map[string]any{"type": "extension_ui_request", "id": "upstream-ui-notify", "method": "notify", "message": "info only", "notifyType": "info"})
		}
		if scenario == "rpc_pause" && cmd.Type == "get_state" {
			_, _ = reader.ReadByte()
			_ = encoder.Encode(map[string]any{"type": "fixture_write_start"})
			time.Sleep(time.Hour)
			return
		}
	}
}
