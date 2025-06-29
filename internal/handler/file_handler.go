// file: internal/handler/file_handler.go
package handler

import (
	"errors" // BARU: Import errors
	"fmt"
	"io"
	"net/http"
	"strings"

	commonjwt "github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

type FileHandler struct {
	fileService service.FileService
}

func NewFileHandler(fs service.FileService) *FileHandler {
	return &FileHandler{fileService: fs}
}

func (h *FileHandler) UploadFile(c *gin.Context) {
	userID, err := commonjwt.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: user ID not found in token"})
		return
	}

	tagsValue := c.PostForm("tags")
	var tags []string
	if tagsValue != "" {
		tags = strings.Split(tagsValue, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file is received"})
		return
	}

	metadata, err := h.fileService.UploadFile(c.Request.Context(), userID, file, tags)
	if err != nil {
		if strings.Contains(err.Error(), "exceeds the limit") || strings.Contains(err.Error(), "is not allowed") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed", "details": err.Error()})
		} else {
			log.Error().Err(err).Msg("Gagal memproses upload file")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process file"})
		}
		return
	}

	c.JSON(http.StatusOK, metadata)
}

func (h *FileHandler) DownloadFile(c *gin.Context) {
	fileID := c.Param("id")

	claimsVal, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Claims tidak ditemukan"})
		return
	}
	claims, ok := claimsVal.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Format claims tidak valid"})
		return
	}

	metadata, err := h.fileService.GetFileMetadata(c.Request.Context(), fileID, claims)
	if err != nil {
		// FIX: Hapus deklarasi `statusCode` yang tidak efektif.
		if errors.Is(err, service.ErrAccessDenied) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Akses ditolak", "details": err.Error()})
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "File tidak ditemukan", "details": err.Error()})
		}
		return
	}

	fileReader, err := h.fileService.GetFileReader(c.Request.Context(), metadata.StoragePath)
	if err != nil {
		log.Error().Err(err).Str("file_id", fileID).Str("storage_path", metadata.StoragePath).Msg("File ada di metadata tapi tidak ditemukan di storage")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "File tidak ditemukan di penyimpanan"})
		return
	}
	// FIX: Periksa error saat menutup fileReader.
	defer func() {
		if err := fileReader.Close(); err != nil {
			log.Warn().Err(err).Str("file_id", fileID).Msg("Gagal menutup file reader setelah download")
		}
	}()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", metadata.OriginalName))
	c.Header("Content-Type", metadata.MimeType)
	c.Header("Content-Length", fmt.Sprintf("%d", metadata.SizeBytes))

	_, err = io.Copy(c.Writer, fileReader)
	if err != nil {
		log.Error().Err(err).Str("file_id", fileID).Msg("Gagal mengirim file ke klien")
	}
}
