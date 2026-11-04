package execbridge

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// Config is sealed and hash-bound, but still decoded strictly: no duplicated
// ASCII-fold aliases, Unicode field aliases, unknown fields, or trailing value.
func decodeSessionConfig(data []byte) (SessionConfig, error) {
	var cfg SessionConfig
	if len(data) == 0 || len(data) > 65536 || !utf8.Valid(data) {
		return cfg, ErrBundle
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if uniqueManifestValue(check, 0) != nil {
		return cfg, ErrBundle
	}
	if _, err := check.Token(); err != io.EOF {
		return cfg, ErrBundle
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&cfg) != nil {
		return SessionConfig{}, ErrBundle
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return SessionConfig{}, ErrBundle
	}
	return cfg, nil
}
func uniqueManifestValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrBundle
	}
	token, err := d.Token()
	if err != nil {
		return ErrBundle
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok {
				return ErrBundle
			}
			for _, r := range key {
				if r > 127 {
					return ErrBundle
				}
			}
			key = strings.ToLower(key)
			if keys[key] {
				return ErrBundle
			}
			keys[key] = true
			if uniqueManifestValue(d, depth+1) != nil {
				return ErrBundle
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrBundle
		}
	case '[':
		for d.More() {
			if uniqueManifestValue(d, depth+1) != nil {
				return ErrBundle
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrBundle
		}
	default:
		return ErrBundle
	}
	return nil
}
