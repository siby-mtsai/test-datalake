package lake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func UploadFile(ctx context.Context, client *s3.Client, bucket, key, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", localPath, err)
	}
	defer f.Close()

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("uploading to s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

func UploadBytes(ctx context.Context, client *s3.Client, bucket, key string, body []byte, contentType string) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("uploading to s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

// GetObjectBytes downloads a single object, returning (nil, false, nil) if it doesn't exist rather
// than an error - used by cmd/trim to tell "this date was never exported" (no manifest) apart from
// a real S3 failure, since the trim gate must never drop a partition on missing evidence.
func GetObjectBytes(ctx context.Context, client *s3.Client, bucket, key string) ([]byte, bool, error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("getting s3://%s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, false, fmt.Errorf("reading s3://%s/%s: %w", bucket, key, err)
	}
	return body, true, nil
}

// SumObjectSizes returns the total byte size of every object under prefix - used by cmd/compact
// to publish the cost dashboard's per-prefix storage metric, since CloudWatch's native S3 metrics
// are bucket-level only.
func SumObjectSizes(ctx context.Context, client *s3.Client, bucket, prefix string) (int64, error) {
	var total int64
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, obj := range page.Contents {
			if obj.Size != nil {
				total += *obj.Size
			}
		}
	}
	return total, nil
}
