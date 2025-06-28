package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStorage adalah implementasi Storage untuk disk lokal.
type LocalStorage struct {
	basePath string
}

func NewLocalStorage(basePath string) *LocalStorage {
	return &LocalStorage{basePath: basePath}
}

func (l *LocalStorage) Save(ctx context.Context, path string, content io.Reader) error {
	fullPath := filepath.Join(l.basePath, path)

	// Pastikan direktori ada
	if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return fmt.Errorf("gagal membuat direktori storage lokal: %w", err)
	}

	dst, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("gagal membuat file tujuan di disk: %w", err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, content)
	return err
}

func (l *LocalStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath := filepath.Join(l.basePath, path)
	return os.Open(fullPath)
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	fullPath := filepath.Join(l.basePath, path)
	return os.Remove(fullPath)
}
