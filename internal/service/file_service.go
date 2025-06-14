package service

import (
	"context"
	"fmt"
	"mime/multipart"
	"path/filepath"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
)

type FileService interface {
	UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (*model.FileMetadata, error)
	GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error)
}

type fileService struct {
	repo        repository.FileRepository
	storagePath string
	cfg         *config.Config
}

func NewFileService(repo repository.FileRepository, cfg *config.Config) FileService {
	return &fileService{
		repo:        repo,
		storagePath: "/storage",
		cfg:         cfg,
	}
}

func (s *fileService) UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (*model.FileMetadata, error) {
	// --- BLOK VALIDASI ---
	// 1. Validasi Ukuran File
	if fileHeader.Size > s.cfg.MaxFileSizeBytes {
		return nil, fmt.Errorf("file size (%d bytes) exceeds the limit of %d bytes", fileHeader.Size, s.cfg.MaxFileSizeBytes)
	}

	// 2. Validasi Tipe MIME
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file for validation: %w", err)
	}
	defer file.Close()

	// Deteksi tipe MIME dari konten file
	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to detect mime type: %w", err)
	}

	// Cek apakah tipe MIME yang terdeteksi ada di dalam daftar yang diizinkan
	if !s.cfg.AllowedMimeTypesMap[mime.String()] {
		return nil, fmt.Errorf("mime type '%s' is not allowed", mime.String())
	}

	// Kembalikan file pointer ke awal setelah dibaca oleh mimetype
	file.Seek(0, 0)

	// --- AKHIR BLOK VALIDASI ---
	// Generate ID unik dan path penyimpanan baru
	fileID := uuid.New().String()
	fileExtension := filepath.Ext(fileHeader.Filename)
	storageFileName := fmt.Sprintf("%s%s", fileID, fileExtension)
	storagePath := filepath.Join(s.storagePath, storageFileName)

	// Buat metadata untuk disimpan di DB
	metadata := &model.FileMetadata{
		ID:           fileID,
		OriginalName: fileHeader.Filename,
		StoragePath:  storagePath,
		MimeType:     fileHeader.Header.Get("Content-Type"),
		SizeBytes:    fileHeader.Size,
		OwnerUserID:  &ownerID,
	}
	metadata.MimeType = mime.String()

	// Simpan metadata ke database
	if err := s.repo.Create(ctx, metadata); err != nil {
		return nil, fmt.Errorf("gagal menyimpan metadata file: %w", err)
	}

	// Jika metadata berhasil disimpan, baru simpan file fisik.
	// Ini adalah pola yang lebih aman.
	// (Implementasi untuk menyimpan ke disk ada di handler untuk kesederhanaan)

	return metadata, nil
}

func (s *fileService) GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	return s.repo.GetByID(ctx, id)
}
