// file: internal/handler/file_handler_test.go

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// FIX: Update MockFileService agar sesuai dengan interface baru
type MockFileService struct {
	mock.Mock
}

func (m *MockFileService) UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader, tags []string) (*model.FileMetadata, error) {
	args := m.Called(ctx, ownerID, fileHeader, tags)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.FileMetadata), args.Error(1)
}
func (m *MockFileService) GetFileMetadata(ctx context.Context, fileID string, claims jwt.MapClaims) (*model.FileMetadata, error) {
	args := m.Called(ctx, fileID, claims)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.FileMetadata), args.Error(1)
}
func (m *MockFileService) GetFileReader(ctx context.Context, path string) (io.ReadCloser, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func createUploadRequest(fileContent string, tags string) (*http.Request, string, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if _, err := writer.CreateFormFile("file", "testfile.txt"); err != nil {
		return nil, "", err
	}
	if tags != "" {
		if err := writer.WriteField("tags", tags); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequest(http.MethodPost, "/upload", body)
	if err != nil {
		return nil, "", err
	}
	return req, writer.FormDataContentType(), nil
}

func TestFileHandler_UploadFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const userIDContextKey = "user_id"
	testUserID := "user-id-from-jwt"

	mockAuthMiddleware := func() gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set(userIDContextKey, testUserID)
			c.Next()
		}
	}

	testCases := []struct {
		name               string
		tags               []string
		tagsString         string
		setupMock          func(mockService *MockFileService)
		expectedStatusCode int
		expectedBody       string
	}{
		{
			name:       "Success - File uploaded with tags",
			tags:       []string{"document", "report"},
			tagsString: "document,report",
			setupMock: func(mockService *MockFileService) {
				mockMetadata := &model.FileMetadata{ID: "new-file-uuid", OriginalName: "testfile.txt"}
				mockService.On("UploadFile", mock.Anything, testUserID, mock.AnythingOfType("*multipart.FileHeader"), []string{"document", "report"}).
					Return(mockMetadata, nil).Once()
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       `"id":"new-file-uuid"`,
		},
		{
			name: "Failure - Validation error from service",
			setupMock: func(mockService *MockFileService) {
				validationError := errors.New("file size exceeds the limit")
				mockService.On("UploadFile", mock.Anything, testUserID, mock.AnythingOfType("*multipart.FileHeader"), mock.Anything).
					Return(nil, validationError).Once()
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedBody:       `"details":"file size exceeds the limit"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router := gin.New()
			mockService := new(MockFileService)
			if tc.setupMock != nil {
				tc.setupMock(mockService)
			}
			handler := NewFileHandler(mockService)

			router.POST("/upload", mockAuthMiddleware(), handler.UploadFile)

			req, boundary, err := createUploadRequest("file content", tc.tagsString)
			assert.NoError(t, err)
			req.Header.Set("Content-Type", boundary)

			router.ServeHTTP(recorder, req)

			assert.Equal(t, tc.expectedStatusCode, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tc.expectedBody)
			mockService.AssertExpectations(t)
		})
	}
}
func TestFileHandler_DownloadFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fileID := "file-abc-123"

	// Middleware tiruan untuk menyuntikkan claims ke context
	mockAuthMiddleware := func() gin.HandlerFunc {
		return func(c *gin.Context) {
			claims := jwt.MapClaims{"sub": "user-test", "role": "user"}
			c.Set("claims", claims)
			c.Next()
		}
	}

	testCases := []struct {
		name               string
		setupMock          func(mockService *MockFileService)
		expectedStatusCode int
		expectedBody       string
	}{
		{
			name: "Success - File downloaded",
			setupMock: func(mockService *MockFileService) {
				metadata := &model.FileMetadata{
					ID:           fileID,
					OriginalName: "report.pdf",
					StoragePath:  "some/path/report.pdf",
					MimeType:     "application/pdf",
					SizeBytes:    12345,
				}
				mockService.On("GetFileMetadata", mock.Anything, fileID, mock.AnythingOfType("jwt.MapClaims")).Return(metadata, nil).Once()

				mockReader := io.NopCloser(strings.NewReader("pdf content"))
				mockService.On("GetFileReader", mock.Anything, metadata.StoragePath).Return(mockReader, nil).Once()
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       "pdf content",
		},
		{
			name: "Failure - Access Denied",
			setupMock: func(mockService *MockFileService) {
				mockService.On("GetFileMetadata", mock.Anything, fileID, mock.AnythingOfType("jwt.MapClaims")).Return(nil, service.ErrAccessDenied).Once()
			},
			expectedStatusCode: http.StatusForbidden,
			expectedBody:       `"details":"akses ditolak"`,
		},
		{
			name: "Failure - File Not Found in Metadata",
			setupMock: func(mockService *MockFileService) {
				mockService.On("GetFileMetadata", mock.Anything, fileID, mock.AnythingOfType("jwt.MapClaims")).Return(nil, errors.New("not found")).Once()
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBody:       `"details":"not found"`,
		},
		{
			name: "Failure - File Not Found in Storage",
			setupMock: func(mockService *MockFileService) {
				metadata := &model.FileMetadata{StoragePath: "missing/file.txt"}
				mockService.On("GetFileMetadata", mock.Anything, fileID, mock.AnythingOfType("jwt.MapClaims")).Return(metadata, nil).Once()
				mockService.On("GetFileReader", mock.Anything, metadata.StoragePath).Return(nil, errors.New("file not on disk")).Once()
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       `"error":"File tidak ditemukan di penyimpanan"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router := gin.New()
			mockService := new(MockFileService)
			tc.setupMock(mockService)
			handler := NewFileHandler(mockService)

			router.GET("/files/:id", mockAuthMiddleware(), handler.DownloadFile)

			req, _ := http.NewRequest(http.MethodGet, "/files/"+fileID, nil)
			router.ServeHTTP(recorder, req)

			assert.Equal(t, tc.expectedStatusCode, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tc.expectedBody)
			mockService.AssertExpectations(t)
		})
	}
}

// BARU: Tambahkan implementasi mock untuk metode yang hilang.
func (m *MockFileService) ProcessImageThumbnails(ctx context.Context, event service.FileUploadedEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}
