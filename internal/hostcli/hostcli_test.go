package hostcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// install.sh has to stand alone for curl | sh, so it carries its own copy.
func TestScriptMatchesInstaller(t *testing.T) {
	raw, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start := strings.Index(s, "<<'CLI'\n")
	end := strings.Index(s, "\nCLI\n")
	if start < 0 || end < start {
		t.Fatal("install.sh has no CLI heredoc")
	}
	if got := s[start+len("<<'CLI'\n") : end+1]; got != string(script) {
		t.Fatal("internal/hostcli/tunploy.sh differs from the script in install.sh; copy it over")
	}
}

func TestInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunploy")

	changed, err := Install(path, "ghcr.io/kwa0x2/tunploy")
	if err != nil || !changed {
		t.Fatalf("first install: %v, %v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Contains(got, []byte("IMAGE=ghcr.io/kwa0x2/tunploy\n")) {
		t.Fatalf("image not filled in:\n%s", got)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", st.Mode())
	}

	if changed, err := Install(path, "ghcr.io/kwa0x2/tunploy"); err != nil || changed {
		t.Fatalf("second install: %v, %v", changed, err)
	}
	if changed, err := Install(path, "ghcr.io/fork/tunploy"); err != nil || !changed {
		t.Fatalf("new image: %v, %v", changed, err)
	}

	os.WriteFile(path, []byte("#!/bin/sh\necho someone else's tunploy\n"), 0o755)
	if _, err := Install(path, "ghcr.io/kwa0x2/tunploy"); err == nil {
		t.Fatal("replaced a program that is not ours")
	}
}
