package storage

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage adalah implementasi Storage untuk S3-compatible object storage.
type S3Storage struct {
	client *s3.Client
	bucket string
}

func NewS3Storage(ctx context.Context, region, endpoint, accessKey, secretKey, bucket string, usePathStyle bool) (*S3Storage, error) {
	// Muat konfigurasi dasar tanpa endpoint resolver global.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("gagal memuat konfigurasi AWS SDK: %w", err)
	}

	// Buat klien S3 dengan opsi kustom.
	// Ini adalah cara yang benar untuk menangani endpoint custom seperti Minio.
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = usePathStyle
		if endpoint != "" {
			// Suntikkan resolver langsung ke opsi klien S3.
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	log.Printf("S3 Storage client berhasil diinisialisasi untuk bucket '%s'", bucket)
	return &S3Storage{
		client: s3Client,
		bucket: bucket,
	}, nil
}

func (s *S3Storage) Save(ctx context.Context, path string, content io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
		Body:   content,
	})
	return err
}

func (s *S3Storage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, path string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})
	return err
}
