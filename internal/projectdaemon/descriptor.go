package projectdaemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func ReadTrustedDescriptor(path string) (*Descriptor, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("project daemon descriptor is not a regular file")
	}
	if got := info.Mode().Perm(); got != 0o600 {
		return nil, fmt.Errorf("project daemon descriptor mode = %04o, want 0600", got)
	}
	if err := validateDescriptorOwner(path, info); err != nil {
		return nil, err
	}
	return ReadDescriptor(path)
}

func ReadDescriptor(path string) (*Descriptor, error) {
	raw, err := os.ReadFile(path)
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
		return fmt.Errorf("descriptor is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create descriptor directory: %w", err)
	}
	raw, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary descriptor: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
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
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace descriptor: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure descriptor: %w", err)
	}
	return nil
}

func RemoveDescriptor(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
