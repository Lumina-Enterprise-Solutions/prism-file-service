// file: internal/handler/file_handler.go
package handler

import (
	"net/http"
	"strings"

	commonjwt "github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/service"
	"github.com/gin-gonic/gin"
)

type FileHandler struct {
	fileService service.FileService
}

func NewFileHandler(fs service.FileService) *FileHandler {
	return &FileHandler{fileService: fs}
}

func (h *FileHandler) UploadFile(c *gin.Context) {
	// Ambil userID dari token JWT yang sudah divalidasi oleh middleware
	userID, err := commonjwt.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: user ID not found in token"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file is received"})
		return
	}

	// Panggil service untuk melakukan SEMUA proses: validasi, simpan metadata, dan simpan file fisik.
	// Service akan menangani rollback jika terjadi kesalahan.
	metadata, err := h.fileService.UploadFile(c.Request.Context(), userID, file)
	if err != nil {
		// Cek apakah ini error validasi yang bisa ditampilkan ke user
		if strings.Contains(err.Error(), "exceeds the limit") || strings.Contains(err.Error(), "is not allowed") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed", "details": err.Error()})
		} else {
			// Untuk error lain, anggap sebagai error server internal
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process file", "details": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, metadata)
}

func (h *FileHandler) DownloadFile(c *gin.Context) {
	fileID := c.Param("id")
	metadata, err := h.fileService.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		// Seharusnya service akan mengembalikan error yang lebih spesifik, misal ErrNotFound
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	c.FileAttachment(metadata.StoragePath, metadata.OriginalName)
}
