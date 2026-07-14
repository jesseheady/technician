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
			// Dev/inspection default only. /tmp is ephemeral (cleared on
			// reboot, and not on the technician_data volume in Docker), so
			// artifacts written here do not survive container removal. For
			// retention, use the "s3" driver in production; a persistent local
			// path (e.g. under /var/lib/technician) is the alternative if the
			// local store ever carries artifacts meant to be kept.
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
