package storage

import (
	"context"
	"io"
)

// Storage mendefinisikan kontrak untuk semua backend penyimpanan file.
type Storage interface {
	// Save menyimpan konten dari reader ke path yang diberikan.
	Save(ctx context.Context, path string, content io.Reader) error
	// Get mengembalikan reader untuk konten file di path yang diberikan.
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	// Delete menghapus file dari path yang diberikan.
	Delete(ctx context.Context, path string) error
}
