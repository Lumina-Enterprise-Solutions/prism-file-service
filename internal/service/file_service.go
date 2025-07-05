package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/storage"
	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

var (
	ErrAccessDenied = fmt.Errorf("akses ditolak")
)

type FileUploadedEvent struct {
	FileID      string `json:"file_id"`
	StoragePath string `json:"storage_path"`
	MimeType    string `json:"mime_type"`
}

const ThumbnailQueueName = "file_thumbnails_queue"

type FileService interface {
	UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader, tags []string) (*model.FileMetadata, error)
	GetFileMetadata(ctx context.Context, fileID string, claims jwt.MapClaims) (*model.FileMetadata, error)
	GetFileReader(ctx context.Context, path string) (io.ReadCloser, error)
	// BARU: Metode untuk worker memproses thumbnail
	ProcessImageThumbnails(ctx context.Context, event FileUploadedEvent) error
}

type fileService struct {
	repo        repository.FileRepository
	storage     storage.Storage
	cfg         *fileserviceconfig.Config
	amqpChannel *amqp091.Channel // BARU: Channel RabbitMQ
}

func NewFileService(repo repository.FileRepository, storage storage.Storage, cfg *fileserviceconfig.Config, ch *amqp091.Channel) FileService {
	return &fileService{
		repo:        repo,
		storage:     storage,
		cfg:         cfg,
		amqpChannel: ch,
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
			if err == nil {
				err = fmt.Errorf("gagal menutup file multipart: %w", closeErr)
			} else {
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
			log.Fatal().Err(rollbackErr).Str("file_id", metadata.ID).Msg("FATAL: METADATA ROLLBACK FAILED. INCONSISTENT STATE.")
		}
		return nil, fmt.Errorf("failed to save file content: %w", err)
	}

	// BARU: Terbitkan event jika file adalah gambar dan channel AMQP tersedia
	if strings.HasPrefix(metadata.MimeType, "image/") && s.amqpChannel != nil {
		event := FileUploadedEvent{
			FileID:      metadata.ID,
			StoragePath: metadata.StoragePath,
			MimeType:    metadata.MimeType,
		}
		if err := s.publishFileUploadedEvent(ctx, event); err != nil {
			log.Error().Err(err).Str("file_id", metadata.ID).Msg("Gagal menerbitkan event pembuatan thumbnail. Proses upload utama tetap berhasil.")
		}
	}

	return metadata, nil
}

// BARU: Metode privat untuk menerbitkan event ke RabbitMQ
func (s *fileService) publishFileUploadedEvent(ctx context.Context, event FileUploadedEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("gagal marshal event: %w", err)
	}

	log.Info().Str("file_id", event.FileID).Msg("Menerbitkan event FileUploaded ke antrian thumbnail")

	return s.amqpChannel.PublishWithContext(ctx,
		"",                 // exchange (default, direct-to-queue)
		ThumbnailQueueName, // routing key (nama antrian)
		false,              // mandatory
		false,              // immediate
		amqp091.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp091.Persistent,
		})
}

// BARU: Metode untuk memproses pembuatan thumbnail. Ini akan dipanggil oleh worker.
func (s *fileService) ProcessImageThumbnails(ctx context.Context, event FileUploadedEvent) error {
	log.Info().Str("file_id", event.FileID).Str("path", event.StoragePath).Msg("Mulai memproses thumbnail")

	originalFileReader, err := s.storage.Get(ctx, event.StoragePath)
	if err != nil {
		return fmt.Errorf("gagal download file asli '%s' dari storage: %w", event.StoragePath, err)
	}
	defer func() {
		if closeErr := originalFileReader.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("path", event.StoragePath).Msg("Gagal menutup file reader setelah pemrosesan thumbnail")
		}
	}()

	img, _, err := image.Decode(originalFileReader)
	if err != nil {
		log.Warn().Err(err).Str("file_id", event.FileID).Msg("Gagal decode gambar, melewati pembuatan thumbnail.")
		return nil // Bukan error fatal, cukup lewati pesan ini.
	}

	sizes := map[string]int{"small": 150, "medium": 600}
	for name, size := range sizes {
		// Buat thumbnail dengan mempertahankan rasio aspek
		thumb := imaging.Resize(img, size, 0, imaging.Lanczos)
		var buf bytes.Buffer

		if err := imaging.Encode(&buf, thumb, imaging.JPEG); err != nil {
			log.Error().Err(err).Str("size", name).Msg("Gagal encode thumbnail ke JPEG")
			continue // Lanjutkan ke ukuran berikutnya jika satu gagal
		}

		thumbPath := fmt.Sprintf("thumbnails/%s/%s.jpg", event.FileID, name)
		if err := s.storage.Save(ctx, thumbPath, &buf); err != nil {
			log.Error().Err(err).Str("path", thumbPath).Msg("Gagal menyimpan thumbnail ke storage")
			continue // Lanjutkan ke ukuran berikutnya
		}
		log.Info().Str("path", thumbPath).Int("size", size).Msg("Thumbnail berhasil disimpan")
	}

	// TODO: Di masa depan, bisa menerbitkan event "ThumbnailsProcessed"
	return nil
}

// BARU: Fungsi untuk memulai worker yang akan berjalan di main.go
func StartThumbnailWorker(ch *amqp091.Channel, handler func(delivery amqp091.Delivery)) error {
	// Deklarasikan antrian. 'durable: true' berarti antrian akan tetap ada walaupun RabbitMQ restart.
	_, err := ch.QueueDeclare(
		ThumbnailQueueName,
		true, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("gagal mendeklarasikan antrian thumbnail: %w", err)
	}

	// Mulai mengkonsumsi pesan dari antrian.
	msgs, err := ch.Consume(
		ThumbnailQueueName,
		"",    // consumer tag (dibuat otomatis oleh RabbitMQ)
		false, // auto-ack: false. Kita akan ack/nack secara manual.
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("gagal register consumer thumbnail: %w", err)
	}

	go func() {
		for d := range msgs {
			handler(d) // Panggil handler yang sebenarnya untuk setiap pesan.
		}
	}()

	return nil
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
