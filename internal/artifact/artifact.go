package artifact

import (
	"context"
	"fmt"
	"io"
)

type Store interface {
	Upload(ctx context.Context, key string, r io.Reader, contentType string) (string, error)
	Close() error
}

func New(driver string, opts map[string]string) (Store, error) {
	switch driver {
	case "local":
		path := opts["path"]
		if path == "" {
			path = "/tmp/technician/artifacts"
		}
		return NewLocalStore(path), nil
	case "s3":
		return NewS3Store(opts["bucket"], opts["region"])
	case "stdout":
		return &StdoutStore{}, nil
	case "none", "":
		return &NoopStore{}, nil
	default:
		return nil, fmt.Errorf("unknown artifact driver: %s", driver)
	}
}

type NoopStore struct{}

func (s *NoopStore) Upload(_ context.Context, _ string, _ io.Reader, _ string) (string, error) {
	return "", nil
}
func (s *NoopStore) Close() error { return nil }
