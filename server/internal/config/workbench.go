package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

type Workbench struct{ Token, DataDir, Program, Provider, Model, Thinking, PiAgentDir string }

func LoadWorkbench(lookup func(string) (string, bool)) (Workbench, error) {
	get := func(k string) string { v, _ := lookup(k); return v }
	c := Workbench{Token: get("KAIWU_WEB_TOKEN")}
	if c.Token == "" {
		return c, nil
	}
	if len(c.Token) < 32 {
		return c, errors.New("KAIWU_WEB_TOKEN: at least 32 bytes required")
	}
	c.DataDir = get("KAIWU_DATA_DIR")
	if c.DataDir == "" {
		cwd, e := os.Getwd()
		if e != nil {
			return c, e
		}
		c.DataDir = filepath.Join(cwd, ".kaiwu")
	}
	if !filepath.IsAbs(c.DataDir) {
		return c, errors.New("KAIWU_DATA_DIR: absolute path required")
	}
	c.Program = get("KAIWU_PI_PROGRAM")
	if c.Program == "" {
		// Resolve only absolute PATH entries; never execute a project-local pi.
		for _, dir := range filepath.SplitList(get("PATH")) {
			if !filepath.IsAbs(dir) {
				continue
			}
			if program, err := exec.LookPath(filepath.Join(dir, "pi")); err == nil {
				c.Program = program
				break
			}
		}
	}
	c.PiAgentDir = get("PI_CODING_AGENT_DIR")
	if c.PiAgentDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			c.PiAgentDir = filepath.Join(home, ".pi", "agent")
		}
	}
	c.Provider = get("KAIWU_PI_PROVIDER")
	c.Model = get("KAIWU_PI_MODEL")
	c.Thinking = get("KAIWU_PI_THINKING")
	return c, c.Validate()
}

func (c Workbench) Validate() error {
	if c.Token == "" {
		return nil
	}
	if len(c.Token) < 32 {
		return errors.New("KAIWU_WEB_TOKEN: at least 32 bytes required")
	}
	if !filepath.IsAbs(c.DataDir) {
		return errors.New("KAIWU_DATA_DIR: absolute path required")
	}
	if c.Program != "" && !filepath.IsAbs(c.Program) {
		return errors.New("KAIWU_PI_PROGRAM: absolute path required")
	}
	if c.PiAgentDir != "" && !filepath.IsAbs(c.PiAgentDir) {
		return errors.New("PI_CODING_AGENT_DIR: absolute path required")
	}
	switch c.Thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return errors.New("pi thinking level invalid")
	}
	return nil
}
