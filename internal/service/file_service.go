// file: internal/service/file_service.go

package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os" // <-- Impor paket os
	"path/filepath"
	"strings"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log" // <-- Impor log
)

type FileService interface {
	UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (*model.FileMetadata, error)
	GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error)
}

type fileService struct {
	repo        repository.FileRepository
	storagePath string
	cfg         *fileserviceconfig.Config
}

func NewFileService(repo repository.FileRepository, cfg *fileserviceconfig.Config) FileService {
	return &fileService{
		repo:        repo,
		storagePath: "/storage",
		cfg:         cfg,
	}
}

func (s *fileService) UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (metadata *model.FileMetadata, err error) {
	// --- VALIDASI ---
	if fileHeader.Size > s.cfg.MaxFileSizeBytes {
		return nil, fmt.Errorf("file size (%d bytes) exceeds the limit of %d bytes", fileHeader.Size, s.cfg.MaxFileSizeBytes)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file for validation: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close multipart file: %w", closeErr)
		}
	}()

	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to detect mime type: %w", err)
	}

	// --- PERBAIKAN LOGIKA VALIDASI MIME DI SINI ---
	detectedMimeStr := mime.String()
	// Ambil tipe dasar, misal dari "text/html; charset=utf-8" menjadi "text/html"
	baseMimeType := strings.Split(detectedMimeStr, ";")[0]

	// Cek apakah tipe dasar ada di dalam map yang diizinkan
	if !s.cfg.AllowedMimeTypesMap[baseMimeType] {
		return nil, fmt.Errorf("mime type '%s' is not allowed", detectedMimeStr)
	}

	// --- PROSES ---
	fileID := uuid.New().String()
	fileExtension := filepath.Ext(fileHeader.Filename)
	storageFileName := fmt.Sprintf("%s%s", fileID, fileExtension)
	storagePath := filepath.Join(s.storagePath, storageFileName)

	metadata = &model.FileMetadata{
		ID:           fileID,
		OriginalName: fileHeader.Filename,
		StoragePath:  storagePath,
		MimeType:     mime.String(),
		SizeBytes:    fileHeader.Size,
		OwnerUserID:  &ownerID,
	}

	// 1. Simpan metadata ke database
	if err = s.repo.Create(ctx, metadata); err != nil {
		return nil, fmt.Errorf("gagal menyimpan metadata file: %w", err)
	}

	// Setelah metadata berhasil dibuat, KEMBALIKAN POINTER FILE KE AWAL sebelum menyimpan.
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek file to beginning before saving: %w", err)
	}

	// 2. Buka file tujuan di disk
	dst, err := os.Create(storagePath)
	if err != nil {
		// Jika gagal membuat file di disk, rollback metadata yang sudah dibuat.
		log.Error().Err(err).Str("file_id", metadata.ID).Msg("Failed to create destination file on disk. Rolling back metadata...")
		if rollbackErr := s.repo.DeleteByID(context.Background(), metadata.ID); rollbackErr != nil {
			log.Fatal().Err(rollbackErr).Str("file_id", metadata.ID).Msg("FATAL: METADATA ROLLBACK FAILED.")
		}
		return nil, fmt.Errorf("failed to create file on storage: %w", err)
	}
	defer func() {
		if closeErr := dst.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close destination file: %w", closeErr)
		}
	}()

	// 3. Salin konten file ke tujuan
	if _, err = io.Copy(dst, file); err != nil {
		// Jika gagal menyalin, rollback metadata.
		log.Error().Err(err).Str("file_id", metadata.ID).Msg("Failed to copy file content to disk. Rolling back metadata...")
		if rollbackErr := s.repo.DeleteByID(context.Background(), metadata.ID); rollbackErr != nil {
			log.Fatal().Err(rollbackErr).Str("file_id", metadata.ID).Msg("FATAL: METADATA ROLLBACK FAILED.")
		}
		return nil, fmt.Errorf("failed to save file content: %w", err)
	}

	return metadata, nil
}

func (s *fileService) GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	return s.repo.GetByID(ctx, id)
}
