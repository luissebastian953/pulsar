// Package blob stores generated images. Dir is the local implementation;
// an S3-compatible store slots in behind the same interface later.
package blob

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Store interface {
	Put(id string, data []byte) error
	Open(id string) (io.ReadCloser, error)
	// Delete removes id's blob and any role-suffixed output variants
	// (id-<role>.png, e.g. pose/regional aux images). Missing files are not
	// an error, so it is safe to call for a job that produced no output.
	Delete(id string) error
}

type Dir struct {
	Root string
}

func NewDir(root string) (*Dir, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Dir{Root: root}, nil
}

// ids are server-generated UUIDs; reject anything else so a crafted id can
// never traverse out of Root.
func (d *Dir) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return "", fmt.Errorf("invalid blob id %q", id)
	}
	return filepath.Join(d.Root, id+".png"), nil
}

func (d *Dir) Put(id string, data []byte) error {
	p, err := d.path(id)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (d *Dir) Open(id string) (io.ReadCloser, error) {
	p, err := d.path(id)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (d *Dir) Delete(id string) error {
	p, err := d.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	// aux outputs (pose/regional roles) are stored as id-<role>.png; id is a
	// validated uuid with no glob metacharacters, so this pattern is safe.
	matches, _ := filepath.Glob(filepath.Join(d.Root, id+"-*.png"))
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
