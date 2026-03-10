package artifact

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type LocalStore struct {
	basePath string
}

func NewLocalStore(basePath string) *LocalStore {
	return &LocalStore{basePath: basePath}
}

func (s *LocalStore) Upload(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	fullPath := filepath.Join(s.basePath, key)

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("creating file %s: %w", fullPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("writing file %s: %w", fullPath, err)
	}

	return fullPath, nil
}

func (s *LocalStore) Close() error { return nil }
