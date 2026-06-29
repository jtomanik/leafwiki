package projectdaemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type descriptorTempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Close() error
}

var (
	errDescriptorRequired       = errors.New("descriptor is required")
	errDescriptorNotRegularFile = errors.New("project daemon descriptor is not a regular file")
	ErrDescriptorSchemaMismatch = errors.New("project daemon descriptor schema version mismatch")

	chmodDescriptorFile        = os.Chmod
	createDescriptorTempFile   = func(dir string, pattern string) (descriptorTempFile, error) { return os.CreateTemp(dir, pattern) }
	marshalDescriptorJSON      = json.MarshalIndent
	mkdirAllDescriptorPath     = os.MkdirAll
	removeDescriptorPath       = os.Remove
	removeTemporaryDescriptor  = os.Remove
	renameTemporaryDescriptor  = os.Rename
	readDescriptorFile         = os.ReadFile
	lstatDescriptorFile        = os.Lstat
	validateDescriptorFileMode = func(info os.FileInfo) os.FileMode { return info.Mode() }
)

func ReadTrustedDescriptor(path string) (*Descriptor, error) {
	info, err := lstatDescriptorFile(path)
	if err != nil {
		return nil, err
	}
	if !validateDescriptorFileMode(info).IsRegular() {
		return nil, errDescriptorNotRegularFile
	}
	if got := validateDescriptorFileMode(info).Perm(); got != 0o600 {
		return nil, fmt.Errorf("project daemon descriptor mode = %04o, want 0600", got)
	}
	if err := validateDescriptorOwner(path, info); err != nil {
		return nil, err
	}
	return ReadDescriptor(path)
}

func ReadDescriptor(path string) (*Descriptor, error) {
	raw, err := readDescriptorFile(path)
	if err != nil {
		return nil, err
	}
	var desc Descriptor
	if err := json.Unmarshal(raw, &desc); err != nil {
		return nil, err
	}
	return &desc, nil
}

func WriteDescriptorAtomic(path string, desc *Descriptor) error {
	if desc == nil {
		return errDescriptorRequired
	}
	if err := mkdirAllDescriptorPath(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create descriptor directory: %w", err)
	}
	raw, err := marshalDescriptorJSON(desc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := createDescriptorTempFile(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary descriptor: %w", err)
	}
	tmpPath := tmp.Name()
	defer removeTemporaryDescriptor(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary descriptor: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary descriptor: %w", err)
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("finish temporary descriptor: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary descriptor: %w", err)
	}
	if err := renameTemporaryDescriptor(tmpPath, path); err != nil {
		return fmt.Errorf("replace descriptor: %w", err)
	}
	if err := chmodDescriptorFile(path, 0o600); err != nil {
		return fmt.Errorf("secure descriptor: %w", err)
	}
	return nil
}

func RemoveDescriptor(path string) error {
	if err := removeDescriptorPath(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
