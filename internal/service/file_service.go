package service

import (
	"context"
	"fmt"
	"io" // <-- PERBAIKAN: Impor paket `io` untuk `SeekStart`
	"mime/multipart"
	"path/filepath"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
)

type FileService interface {
	UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (*model.FileMetadata, error)
	GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error)
	DeleteMetadata(ctx context.Context, id string) error
}

type fileService struct {
	repo        repository.FileRepository
	storagePath string
	cfg         *fileserviceconfig.Config
}

func NewFileService(repo repository.FileRepository, cfg *fileserviceconfig.Config) FileService {
	return &fileService{
		repo:        repo,
		storagePath: "/app/storage", // <-- PERBAIKAN: Sesuaikan dengan path di Dockerfile
		cfg:         cfg,
	}
}

// PERBAIKAN: Gunakan named return value `err` untuk menangani error dari defer file.Close()
func (s *fileService) UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (metadata *model.FileMetadata, err error) {
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
	// PERBAIKAN: Tangani error dari `file.Close()`
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close file: %w", closeErr)
		}
	}()

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
	// PERBAIKAN: Tangani error dari `file.Seek()`
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek file to beginning: %w", err)
	}

	// --- AKHIR BLOK VALIDASI ---
	// Generate ID unik dan path penyimpanan baru
	fileID := uuid.New().String()
	fileExtension := filepath.Ext(fileHeader.Filename)
	storageFileName := fmt.Sprintf("%s%s", fileID, fileExtension)
	storagePath := filepath.Join(s.storagePath, storageFileName)

	// Buat metadata untuk disimpan di DB
	metadata = &model.FileMetadata{ // PERBAIKAN: Gunakan operator `=` bukan `:=` karena `metadata` sudah dideklarasikan di return signature
		ID:           fileID,
		OriginalName: fileHeader.Filename,
		StoragePath:  storagePath,
		MimeType:     mime.String(), // PERBAIKAN: Gunakan mime yang terdeteksi, bukan dari header klien
		SizeBytes:    fileHeader.Size,
		OwnerUserID:  &ownerID,
	}

	// Simpan metadata ke database
	if err := s.repo.Create(ctx, metadata); err != nil {
		return nil, fmt.Errorf("gagal menyimpan metadata file: %w", err)
	}

	// Jika metadata berhasil disimpan, baru simpan file fisik.
	// (Implementasi untuk menyimpan ke disk ada di handler untuk kesederhanaan)

	return metadata, nil // `err` akan nil di sini jika semua berhasil
}

func (s *fileService) GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	return s.repo.GetByID(ctx, id)
}
func (s *fileService) DeleteMetadata(ctx context.Context, id string) error {
	return s.repo.DeleteByID(ctx, id) // Asumsi metode ini akan kita tambahkan ke repo
}
