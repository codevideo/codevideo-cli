package cloud

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// CodeVideo's own bucket, which the hosted API's render worker uploads to.
const (
	defaultBucket = "fullstackcraft"
	defaultRegion = "us-east-1"
)

// Config is where the CLI uploads files. The keys have no default; the bucket
// and region default to CodeVideo's own, so a worker that only sets the keys
// keeps uploading where it always has.
type Config struct {
	KeyID  string
	Secret string
	Bucket string
	Region string
}

// ConfigFromEnv reads CODEVIDEO_S3_KEY_ID, CODEVIDEO_S3_SECRET,
// CODEVIDEO_S3_BUCKET and CODEVIDEO_S3_REGION.
func ConfigFromEnv() Config {
	c := Config{
		KeyID:  os.Getenv("CODEVIDEO_S3_KEY_ID"),
		Secret: os.Getenv("CODEVIDEO_S3_SECRET"),
		Bucket: strings.TrimSpace(os.Getenv("CODEVIDEO_S3_BUCKET")),
		Region: strings.TrimSpace(os.Getenv("CODEVIDEO_S3_REGION")),
	}
	if c.Bucket == "" {
		c.Bucket = defaultBucket
	}
	if c.Region == "" {
		c.Region = defaultRegion
	}
	return c
}

// Enabled reports whether both keys are set, so uploads can work.
func (c Config) Enabled() bool {
	return c.KeyID != "" && c.Secret != ""
}

// Key is where a file goes in the bucket.
func Key(path, filename string) string {
	return fmt.Sprintf("codevideo/%s/%s", path, filename)
}

// URL is an object's public address. A bucket name with dots gets a
// path-style URL, because S3's TLS certificate doesn't cover dotted
// virtual-hosted names.
func (c Config) URL(key string) string {
	if strings.Contains(c.Bucket, ".") {
		return fmt.Sprintf("https://s3.%s.amazonaws.com/%s/%s", c.Region, c.Bucket, key)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.Bucket, c.Region, key)
}

// Upload puts a file buffer in the bucket and returns its public URL.
func (c Config) Upload(ctx context.Context, buffer []byte, path string, filename string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("S3 credentials are not set")
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(c.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.KeyID, c.Secret, "")),
	)
	if err != nil {
		return "", fmt.Errorf("failed to load configuration: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	key := Key(path, filename)

	// Perform the PutObject request.
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(buffer),
	})
	if err != nil {
		return "", fmt.Errorf("error uploading object to %s: %w", c.Bucket, err)
	}

	return c.URL(key), nil
}

// UploadFileToS3 uploads a file buffer to the bucket configured in the
// environment (ConfigFromEnv) and returns its public URL.
func UploadFileToS3(ctx context.Context, buffer []byte, path string, filename string) (string, error) {
	return ConfigFromEnv().Upload(ctx, buffer, path, filename)
}
