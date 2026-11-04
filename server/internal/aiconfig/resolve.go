package aiconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

func Decode(b []byte) (Config, error) {
	var c Config
	if len(b) > 256<<10 {
		return c, errors.New("AI configuration too large")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, errors.New("invalid AI configuration fields")
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("trailing AI configuration data")
	}
	if c.SchemaVersion != 1 {
		return c, errors.New("unsupported AI configuration schema")
	}
	return c, nil
}
func Resolve(global, project Config) (Resolved, error) {
	r := Resolved{SchemaVersion: 1, Adapter: "pi", Rules: []string{}, Skills: []string{}, Sources: map[string]string{}}
	for _, key := range []string{"adapter", "profile.provider", "profile.model", "profile.thinking", "rules", "skills", "credentialRef"} {
		r.Sources[key] = "builtin"
	}
	for index, c := range []Config{global, project} {
		source := "global"
		if index == 1 {
			source = "project"
		}
		if c.SchemaVersion != 1 {
			return r, errors.New("unsupported AI configuration schema")
		}
		if c.Adapter != nil {
			if *c.Adapter != "pi" {
				return r, errors.New("only pi adapter is supported")
			}
			r.Adapter = *c.Adapter
			r.Sources["adapter"] = source
		}
		if c.Profile != nil {
			for key, pair := range map[string]struct {
				value  *string
				target *string
			}{"provider": {c.Profile.Provider, &r.Profile.Provider}, "model": {c.Profile.Model, &r.Profile.Model}, "thinking": {c.Profile.Thinking, &r.Profile.Thinking}} {
				if pair.value == nil {
					continue
				}
				if !utf8.ValidString(*pair.value) || len(*pair.value) > 300 || strings.ContainsAny(*pair.value, "\r\n\x00") {
					return r, errors.New("invalid AI profile")
				}
				if index == 1 && key == "provider" && *pair.value != r.Profile.Provider {
					return r, errors.New("project configuration cannot authorize another credential provider")
				}
				*pair.target = *pair.value
				r.Sources["profile."+key] = source
			}
		}
		if c.CredentialRef != nil {
			if *c.CredentialRef != "" && *c.CredentialRef != "server_pi" {
				return r, errors.New("unsupported credential reference")
			}
			if index == 1 && *c.CredentialRef != "" && *c.CredentialRef != r.CredentialRef {
				return r, errors.New("project cannot enable credentials")
			}
			r.CredentialRef = *c.CredentialRef
			r.Sources["credentialRef"] = source
		}
		if c.Rules != nil {
			r.Rules = append([]string{}, (*c.Rules)...)
			r.Sources["rules"] = source
		}
		if len(r.Rules) > 32 {
			return r, errors.New("too many rules")
		}
		total := 0
		for _, rule := range r.Rules {
			total += len(rule)
			if !utf8.ValidString(rule) || strings.ContainsRune(rule, 0) {
				return r, errors.New("invalid rule")
			}
		}
		if total > 64<<10 {
			return r, errors.New("rules too large")
		}
		if c.Skills != nil {
			allowed := map[string]bool{}
			for _, skill := range r.Skills {
				allowed[skill] = true
			}
			if len(*c.Skills) > 32 {
				return r, errors.New("too many skills")
			}
			for _, skill := range *c.Skills {
				if skill == "" || len(skill) > 80 || strings.ContainsAny(skill, "/\\:.\r\n\x00") || strings.TrimSpace(skill) != skill {
					return r, errors.New("invalid skill identifier")
				}
				if index == 1 && !allowed[skill] {
					return r, errors.New("project cannot authorize a skill")
				}
			}
			r.Skills = append([]string{}, (*c.Skills)...)
			r.Sources["skills"] = source
		}
	}
	switch r.Profile.Thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return r, errors.New("invalid thinking level")
	}
	if r.CredentialRef == "server_pi" && strings.TrimSpace(r.Profile.Provider) == "" {
		return Resolved{}, errors.New("server_pi requires a selected provider and model")
	}
	if (r.Profile.Provider == "") != (r.Profile.Model == "") {
		return r, errors.New("provider and model must be selected together")
	}
	r.Profile.CredentialSource = r.CredentialRef
	// Include both normalized inputs: changing a template records provenance even
	// when the current project overrides the changed field.
	b, _ := json.Marshal(struct{ Global, Project Config }{global, project})
	sum := sha256.Sum256(b)
	r.Version = hex.EncodeToString(sum[:])
	return r, nil
}
