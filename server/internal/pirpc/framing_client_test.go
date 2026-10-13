package pirpc

import (
	"errors"
	"testing"
)

// Unit decoder REDs cover these validators; this matrix checks their wiring to
// the real owned pipes and Client terminal state, not a second decoder.
func TestClientMalformedStdout(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		want error
	}{{"fixture_bad_json", ErrProtocol}, {"fixture_bad_utf8", ErrProtocol}, {"fixture_truncated", ErrProtocol}, {"fixture_large_record", ErrRecordTooLarge}} {
		t.Run(tc.typ, func(t *testing.T) {
			opts := testOptions()
			opts.MaxRecordBytes = 512
			c := startClient(t, "rpc", opts)
			_, err := c.Call(callContext(t), Command{Type: tc.typ})
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			r, err := c.Wait(callContext(t))
			if err != nil || !r.Reaped || !errors.Is(r.Cause, tc.want) {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}
