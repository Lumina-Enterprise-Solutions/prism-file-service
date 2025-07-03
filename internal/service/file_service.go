package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/storage"
	"github.com/gabriel-vasile/mimetype"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var (
	ErrAccessDenied = fmt.Errorf("akses ditolak")
)

type FileService interface {
	UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader, tags []string) (*model.FileMetadata, error)
	GetFileMetadata(ctx context.Context, fileID string, claims jwt.MapClaims) (*model.FileMetadata, error)
	GetFileReader(ctx context.Context, path string) (io.ReadCloser, error)
}

type fileService struct {
	repo    repository.FileRepository
	storage storage.Storage
	cfg     *fileserviceconfig.Config
}

func NewFileService(repo repository.FileRepository, storage storage.Storage, cfg *fileserviceconfig.Config) FileService {
	return &fileService{
		repo:    repo,
		storage: storage,
		cfg:     cfg,
	}
}

func (s *fileService) UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader, tags []string) (metadata *model.FileMetadata, err error) {
	if fileHeader.Size > s.cfg.MaxFileSizeBytes {
		return nil, fmt.Errorf("file size (%d bytes) exceeds the limit of %d bytes", fileHeader.Size, s.cfg.MaxFileSizeBytes)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file for validation: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			// Jika belum ada error lain, jadikan ini error utama.
			if err == nil {
				err = fmt.Errorf("gagal menutup file multipart: %w", closeErr)
			} else {
				// Jika sudah ada error, log ini sebagai peringatan.
				log.Warn().Err(closeErr).Msg("Gagal menutup file multipart setelah error sebelumnya.")
			}
		}
	}()

	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to detect mime type: %w", err)
	}

	baseMimeType := strings.Split(mime.String(), ";")[0]
	if !s.cfg.AllowedMimeTypesMap[baseMimeType] {
		return nil, fmt.Errorf("mime type '%s' is not allowed", mime.String())
	}

	fileID := uuid.New().String()
	fileExtension := filepath.Ext(fileHeader.Filename)
	storageFileName := fmt.Sprintf("%s%s", fileID, fileExtension)

	metadata = &model.FileMetadata{
		ID:           fileID,
		OriginalName: fileHeader.Filename,
		StoragePath:  storageFileName,
		MimeType:     mime.String(),
		SizeBytes:    fileHeader.Size,
		OwnerUserID:  &ownerID,
	}

	if err = s.repo.Create(ctx, metadata, tags); err != nil {
		return nil, fmt.Errorf("gagal menyimpan metadata file: %w", err)
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek file to beginning before saving: %w", err)
	}

	if err = s.storage.Save(ctx, storageFileName, file); err != nil {
		log.Error().Err(err).Str("file_id", metadata.ID).Msg("Gagal menyimpan file ke storage. Rollback metadata...")
		if rollbackErr := s.repo.DeleteByID(ctx, metadata.ID); rollbackErr != nil {
			log.Fatal().Err(rollbackErr).Str("file_id", metadata.ID).Msg("FATAL: METADATA ROLLBACK FAILED.")
		}
		return nil, fmt.Errorf("failed to save file content: %w", err)
	}

	return metadata, nil
}

func (s *fileService) GetFileMetadata(ctx context.Context, fileID string, claims jwt.MapClaims) (*model.FileMetadata, error) {
	metadata, err := s.repo.GetByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	userID := claims["sub"].(string)
	userRole, _ := claims["role"].(string)

	if metadata.OwnerUserID != nil && *metadata.OwnerUserID == userID {
		return metadata, nil
	}

	if userRole == "admin" {
		return metadata, nil
	}

	if len(metadata.Tags) > 0 {
		hasAccess, err := s.repo.CheckRoleAccess(ctx, fileID, userRole)
		if err != nil {
			log.Error().Err(err).Str("file_id", fileID).Str("role", userRole).Msg("Gagal memeriksa akses peran")
			return nil, ErrAccessDenied
		}
		if hasAccess {
			return metadata, nil
		}
	}

	return nil, ErrAccessDenied
}

func (s *fileService) GetFileReader(ctx context.Context, path string) (io.ReadCloser, error) {
	return s.storage.Get(ctx, path)
}
