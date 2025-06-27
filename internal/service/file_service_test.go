// file: internal/service/file_service_test.go

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"testing"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockFileRepository adalah tiruan dari FileRepository.
// Ini memungkinkan kita untuk mengontrol perilakunya dalam tes tanpa memerlukan database.
type MockFileRepository struct {
	mock.Mock
}

// Implementasikan metode-metode dari interface FileRepository
func (m *MockFileRepository) Create(ctx context.Context, metadata *model.FileMetadata) error {
	args := m.Called(ctx, metadata)
	return args.Error(0)
}

func (m *MockFileRepository) GetByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.FileMetadata), args.Error(1)
}

func (m *MockFileRepository) DeleteByID(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// createTestFileHeader adalah helper untuk membuat file tiruan untuk diuji.
func createTestFileHeader(content string, filename string) (*multipart.FileHeader, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, bytes.NewBufferString(content)); err != nil {
		return nil, err
	}
	// PERBAIKAN 5: Cek error dari Close
	if err := writer.Close(); err != nil {
		return nil, err
	}

	reader := multipart.NewReader(body, writer.Boundary())
	form, err := reader.ReadForm(1024 * 10)
	if err != nil {
		return nil, err
	}

	return form.File["file"][0], nil
}

func TestFileService_UploadFile(t *testing.T) {
	// Buat direktori storage sementara untuk tes ini
	tempDir := t.TempDir()

	testConfig := &fileserviceconfig.Config{
		MaxFileSizeBytes: 5 * 1024 * 1024,
		// Map ini SEKARANG harus berisi tipe dasar, tanpa charset.
		AllowedMimeTypesMap: map[string]bool{
			"image/png":  true,
			"image/jpeg": true,
			"text/plain": true,
		},
	}
	ownerID := "test-user-123"

	testCases := []struct {
		name          string
		fileContent   string
		fileName      string
		config        *fileserviceconfig.Config
		setupMock     func(mockRepo *MockFileRepository)
		expectError   bool
		expectedError string
	}{
		{
			name:        "Success - Valid PNG file",
			fileContent: "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR...", // Magic bytes untuk PNG
			fileName:    "test.png",
			config:      testConfig,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.FileMetadata")).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name:          "Error - File size exceeds limit",
			fileContent:   string(make([]byte, 6*1024*1024)),
			fileName:      "large_file.png",
			config:        testConfig,
			setupMock:     func(mockRepo *MockFileRepository) { /* Tidak ada interaksi repo */ },
			expectError:   true,
			expectedError: "exceeds the limit",
		},
		{
			name:          "Error - Mime type not allowed",
			fileContent:   "<html><body>hello</body></html>",
			fileName:      "test.html",
			config:        testConfig,
			setupMock:     func(mockRepo *MockFileRepository) { /* Tidak ada interaksi repo */ },
			expectError:   true,
			expectedError: "mime type 'text/html; charset=utf-8' is not allowed",
		},
		{
			name:        "Error - Database fails to save metadata",
			fileContent: "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR...",
			fileName:    "test.png",
			config:      testConfig,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.FileMetadata")).
					Return(errors.New("database connection lost")).
					Once()
			},
			expectError:   true,
			expectedError: "gagal menyimpan metadata file",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := new(MockFileRepository)
			if tc.setupMock != nil {
				tc.setupMock(mockRepo)
			}

			service := &fileService{
				repo:        mockRepo,
				storagePath: tempDir,
				cfg:         tc.config,
			}

			fileHeader, err := createTestFileHeader(tc.fileContent, tc.fileName)
			assert.NoError(t, err)

			metadata, err := service.UploadFile(context.Background(), ownerID, fileHeader)

			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
				assert.Nil(t, metadata)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, metadata)
				assert.Equal(t, tc.fileName, metadata.OriginalName)
				assert.FileExists(t, metadata.StoragePath)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}
