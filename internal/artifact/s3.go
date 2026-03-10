package artifact

import (
	"context"
	"fmt"
	"io"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Store struct {
	client *s3.Client
	bucket string
}

func NewS3Store(bucket, region string) (*S3Store, error) {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	return &S3Store{
		client: client,
		bucket: bucket,
	}, nil
}

func (s *S3Store) Upload(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	input := &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   r,
	}
	if contentType != "" {
		input.ContentType = &contentType
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return "", fmt.Errorf("uploading to S3: %w", err)
	}

	return fmt.Sprintf("s3://%s/%s", s.bucket, key), nil
}

func (s *S3Store) Close() error { return nil }
