package artifact

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

type StdoutStore struct{}

func (s *StdoutStore) Upload(_ context.Context, key string, r io.Reader, contentType string) (string, error) {
	fmt.Fprintf(os.Stderr, "--- Artifact: %s (type: %s) ---\n", key, contentType)

	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("reading artifact data: %w", err)
	}

	// For text-based content, write directly; otherwise base64
	if isTextContent(contentType) {
		os.Stdout.Write(data)
	} else {
		encoded := base64.StdEncoding.EncodeToString(data)
		fmt.Fprintln(os.Stdout, encoded)
	}

	fmt.Fprintf(os.Stderr, "--- End artifact: %s ---\n", key)
	return "stdout://" + key, nil
}

func (s *StdoutStore) Close() error { return nil }

func isTextContent(ct string) bool {
	for _, prefix := range []string{"text/", "application/json", "application/xml"} {
		if len(ct) >= len(prefix) && ct[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
