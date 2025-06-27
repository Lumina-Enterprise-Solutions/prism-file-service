// file: internal/handler/file_handler_test.go

package handler

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockFileService tetap sama
type MockFileService struct {
	mock.Mock
}

func (m *MockFileService) UploadFile(ctx context.Context, ownerID string, fileHeader *multipart.FileHeader) (*model.FileMetadata, error) {
	args := m.Called(ctx, ownerID, fileHeader)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.FileMetadata), args.Error(1)
}

func (m *MockFileService) GetFileByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.FileMetadata), args.Error(1)
}

// file: internal/handler/file_handler_test.go

// ... (import dan mock service tetap sama)

func createUploadRequest(fileContent string) (*http.Request, string, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "testfile.txt")
	if err != nil {
		return nil, "", err
	}
	// PERBAIKAN 1: Cek error dari Write
	if _, err := part.Write([]byte(fileContent)); err != nil {
		return nil, "", err
	}
	// PERBAIKAN 2: Cek error dari Close
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

	// INILAH KUNCI PERBAIKANNYA.
	// Kunci ini harus sama persis dengan yang digunakan di dalam common-libs/auth/jwt.go
	const userIDContextKey = "user_id" // Menggunakan kunci yang benar: "user_id"
	testUserID := "user-id-from-jwt"

	// Buat middleware tiruan untuk menyuntikkan userID ke dalam konteks request
	mockAuthMiddleware := func() gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set(userIDContextKey, testUserID) // Set dengan kunci yang benar
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
			name: "Success - File uploaded correctly",
			setupMock: func(mockService *MockFileService) {
				mockMetadata := &model.FileMetadata{ID: "new-file-uuid", OriginalName: "testfile.txt"}
				mockService.On("UploadFile", mock.Anything, testUserID, mock.AnythingOfType("*multipart.FileHeader")).
					Return(mockMetadata, nil).Once()
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       `"id":"new-file-uuid"`,
		},
		{
			name: "Failure - Validation error from service",
			setupMock: func(mockService *MockFileService) {
				validationError := errors.New("file size exceeds the limit")
				mockService.On("UploadFile", mock.Anything, testUserID, mock.AnythingOfType("*multipart.FileHeader")).
					Return(nil, validationError).Once()
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedBody:       `"details":"file size exceeds the limit"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			recorder := httptest.NewRecorder()
			router := gin.New()

			mockService := new(MockFileService)
			if tc.setupMock != nil {
				tc.setupMock(mockService)
			}
			handler := NewFileHandler(mockService)

			// Terapkan middleware tiruan DAN handler ke rute
			router.POST("/upload", mockAuthMiddleware(), handler.UploadFile)

			req, boundary, err := createUploadRequest("file content")
			assert.NoError(t, err)
			req.Header.Set("Content-Type", boundary)

			router.ServeHTTP(recorder, req)

			// Assert
			assert.Equal(t, tc.expectedStatusCode, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tc.expectedBody)
			mockService.AssertExpectations(t)
		})
	}
}
