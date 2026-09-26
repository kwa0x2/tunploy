// Package hostcli holds the tunploy command the install script puts on the
// server, so a panel that updated itself can put it there too.
package hostcli

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Path is where the install script puts the command on the server.
const Path = "/usr/local/bin/tunploy"

// A copy of the script in install.sh; a test keeps the two equal.
//
//go:embed tunploy.sh
var script []byte

// Only a file with this line is Tunploy's to replace.
var marker = []byte("# Manages the Tunploy panel on this server.")

func Script(image string) []byte {
	return bytes.ReplaceAll(script, []byte("@IMAGE@"), []byte(image))
}

// Install writes the command to path unless it is already there as written,
// or path holds some other program of the same name.
func Install(path, image string) (bool, error) {
	want := Script(image)
	have, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return false, err
	case bytes.Equal(have, want):
		return false, nil
	case !bytes.Contains(have, marker):
		return false, fmt.Errorf("%s is another program; leaving it alone", path)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tunploy-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(want); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}
