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
	// Ambil userID dari token JWT
	userID, err := commonjwt.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file is received"})
		return
	}

	// Panggil service untuk membuat metadata
	metadata, err := h.fileService.UploadFile(c.Request.Context(), userID, file)
	if err != nil {
		if strings.Contains(err.Error(), "exceeds the limit") || strings.Contains(err.Error(), "is not allowed") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed", "details": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process file metadata", "details": err.Error()})
		}
		return
	}

	// Simpan file ke disk menggunakan path dari metadata
	if err := c.SaveUploadedFile(file, metadata.StoragePath); err != nil {
		// TODO: Rollback - hapus entri metadata dari DB jika ini gagal
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save the file"})
		return
	}

	c.JSON(http.StatusOK, metadata)
}

func (h *FileHandler) DownloadFile(c *gin.Context) {
	fileID := c.Param("id")
	metadata, err := h.fileService.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// Kirim file dengan nama aslinya untuk di-download
	c.FileAttachment(metadata.StoragePath, metadata.OriginalName)
}

// // UploadFile menangani request upload file tunggal.
// func (h *FileHandler) UploadFile(c *gin.Context) {
// 	// Ambil file dari form-data dengan key "file"
// 	file, err := c.FormFile("file")
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": "No file is received"})
// 		return
// 	}

// 	// Tentukan path penyimpanan.
// 	// Di dalam kontainer, kita akan menyimpannya di direktori /storage.
// 	// Direktori ini akan kita map ke volume Docker.
// 	storagePath := "/storage"
// 	destination := filepath.Join(storagePath, file.Filename)

// 	// Simpan file yang di-upload ke path tujuan.
// 	if err := c.SaveUploadedFile(file, destination); err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to save the file"})
// 		return
// 	}

// 	// Berikan response sukses
// 	c.JSON(http.StatusOK, gin.H{
// 		"message":  fmt.Sprintf("File '%s' uploaded successfully.", file.Filename),
// 		"filename": file.Filename,
// 		"size":     file.Size,
// 		"path":     destination, // Path di dalam kontainer
// 	})
// }

// // DownloadFile menangani request download file.
// func (h *FileHandler) DownloadFile(c *gin.Context) {
// 	// Ambil nama file dari parameter URL.
// 	filename := c.Param("filename")
// 	if filename == "" {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": "Filename is required"})
// 		return
// 	}

// 	// Tentukan path file di dalam storage kontainer.
// 	storagePath := "/storage"
// 	filePath := filepath.Join(storagePath, filename)

// 	// Set header agar browser tahu ini adalah file download, bukan untuk ditampilkan.
// 	// `Content-Disposition: attachment` memaksa browser untuk men-download.
// 	c.Header("Content-Description", "File Transfer")
// 	c.Header("Content-Transfer-Encoding", "binary")
// 	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
// 	c.Header("Content-Type", "application/octet-stream")

// 	// Kirim file sebagai response. Gin akan menangani streaming file secara efisien.
// 	c.File(filePath)
// }
