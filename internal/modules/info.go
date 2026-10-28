package modules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tivjnb/futi/internal/metayaml"
)

// Info describes an installed or hand-authored module: its root directory and
// the __futi_meta.yaml contents.
type Info struct {
	Root string
	Meta *metayaml.Meta // nil: no __futi_meta.yaml
}

// Path returns the directory of the module name.
func (m *Manager) Path(name string) (string, error) {
	if err := ValidateResolveName(name); err != nil {
		return "", err
	}
	source, err := m.rt.Source()
	if err != nil {
		return "", err
	}
	manifestPath, _, err := source.Resolve(name)
	if err != nil {
		return "", fmt.Errorf("modules path: %w", err)
	}
	return filepath.Dir(manifestPath), nil
}

// Info resolves the module name and reads its __futi_meta.yaml. A module
// without a sidecar is not an error: Meta is nil. When the sidecar is
// unreadable, Info.Root is still set next to the error.
func (m *Manager) Info(name string) (Info, error) {
	if err := ValidateResolveName(name); err != nil {
		return Info{}, err
	}
	source, err := m.rt.Source()
	if err != nil {
		return Info{}, err
	}
	manifestPath, root, err := source.Resolve(name)
	if err != nil {
		return Info{}, fmt.Errorf("modules info: %w", err)
	}

	metaPath := filepath.Join(filepath.Dir(manifestPath), metayaml.FileName)
	if _, err := os.Stat(metaPath); err != nil {
		return Info{Root: root}, nil
	}
	meta, err := metayaml.Read(metaPath)
	if err != nil {
		return Info{Root: root}, fmt.Errorf("modules info: %w", err)
	}
	return Info{Root: root, Meta: meta}, nil
}
