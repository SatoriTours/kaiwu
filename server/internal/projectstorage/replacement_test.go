package projectstorage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Replacing a checked ancestor while Git runs must not register the new path.
// Git receives a pinned descriptor, so it must not write into the substitute.
func TestCreateRejectsPathReplacementDuringGit(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, replace := range []string{"root", "container", "repo", ".kaiwu"} {
		t.Run(replace, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "projects")
			outside := t.TempDir()
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			target := root
			switch replace {
			case "container":
				target = filepath.Join(root, "safe")
			case "repo":
				target = filepath.Join(root, "safe", "repo")
			case ".kaiwu":
				target = filepath.Join(root, "safe", ".kaiwu")
			}
			bin := t.TempDir()
			script := "#!/bin/sh\nset -eu\nmv -- " + strconv.Quote(target) + " " + strconv.Quote(target+".moved") + "\nln -s -- " + strconv.Quote(outside) + " " + strconv.Quote(target) + "\nexec " + strconv.Quote(realGit) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := Create(ctx, root, "project-one", "safe"); err == nil {
				t.Fatal("registered substituted path")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("substitute modified: %v %v", entries, err)
			}
		})
	}
}
