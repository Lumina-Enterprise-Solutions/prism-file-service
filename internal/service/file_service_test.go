// file: internal/service/file_service_test.go

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/storage"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mock untuk FileRepository ---
type MockFileRepository struct {
	mock.Mock
}

func (m *MockFileRepository) Create(ctx context.Context, metadata *model.FileMetadata, tags []string) error {
	args := m.Called(ctx, metadata, tags)
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

// FIX: Tambahkan metode CheckRoleAccess ke mock
func (m *MockFileRepository) CheckRoleAccess(ctx context.Context, fileID string, roleName string) (bool, error) {
	args := m.Called(ctx, fileID, roleName)
	return args.Bool(0), args.Error(1)
}

// --- Mock untuk Storage ---
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) Save(ctx context.Context, path string, content io.Reader) error {
	args := m.Called(ctx, path, content)
	return args.Error(0)
}

func (m *MockStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockStorage) Delete(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

var _ storage.Storage = (*MockStorage)(nil)

// createTestFileHeader helper
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
	testConfig := &fileserviceconfig.Config{
		MaxFileSizeBytes:    5 * 1024 * 1024,
		AllowedMimeTypesMap: map[string]bool{"image/png": true, "text/plain": true},
	}
	ownerID := "test-user-123"

	testCases := []struct {
		name        string
		fileContent string
		fileName    string
		tags        []string
		// Config sekarang akan diambil dari variabel di atas, bukan didefinisikan per kasus
		setupMock     func(mockRepo *MockFileRepository, mockStore *MockStorage)
		expectError   bool
		expectedError string
	}{
		{
			name:        "Success - Valid PNG file with tags",
			fileContent: "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR...", // Konten PNG minimal
			fileName:    "test.png",
			tags:        []string{"avatar", "profile"},
			setupMock: func(mockRepo *MockFileRepository, mockStore *MockStorage) {
				mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.FileMetadata"), []string{"avatar", "profile"}).Return(nil).Once()
				mockStore.On("Save", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(nil).Once()
			},
			expectError: false,
		},
		{
			name:          "Error - File too large",
			fileContent:   "file content is too large",
			fileName:      "large.txt",
			tags:          nil,
			setupMock:     nil, // Tidak ada mock yang perlu dipanggil
			expectError:   true,
			expectedError: "exceeds the limit",
		},
		{
			name:          "Error - MIME type not allowed",
			fileContent:   "<html><body>hello</body></html>",
			fileName:      "test.html",
			tags:          nil,
			setupMock:     nil, // Tidak ada mock yang perlu dipanggil
			expectError:   true,
			expectedError: "is not allowed",
		},
		{
			name:        "Error - Database fails to save metadata",
			fileContent: "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR...",
			fileName:    "test.png",
			tags:        nil,
			setupMock: func(mockRepo *MockFileRepository, mockStore *MockStorage) {
				mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.FileMetadata"), mock.Anything).
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
			mockStore := new(MockStorage)
			if tc.setupMock != nil {
				tc.setupMock(mockRepo, mockStore)
			}

			// FIX: Gunakan config yang sudah diinisialisasi
			service := NewFileService(mockRepo, mockStore, testConfig, nil)

			// Untuk kasus file terlalu besar, kita perlu membuat header dengan ukuran palsu
			var fileHeader *multipart.FileHeader
			var err error
			if tc.name == "Error - File too large" {
				// Buat file header dummy dengan ukuran besar
				fileHeader = &multipart.FileHeader{
					Filename: tc.fileName,
					Size:     testConfig.MaxFileSizeBytes + 1, // Pastikan lebih besar dari limit
				}
			} else {
				fileHeader, err = createTestFileHeader(tc.fileContent, tc.fileName)
				require.NoError(t, err)
			}

			metadata, err := service.UploadFile(context.Background(), ownerID, fileHeader, tc.tags)

			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
				assert.Nil(t, metadata)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, metadata)
				assert.Equal(t, tc.fileName, metadata.OriginalName)
			}

			mockRepo.AssertExpectations(t)
			mockStore.AssertExpectations(t)
		})
	}
}
func TestFileService_GetFileMetadata(t *testing.T) {
	ctx := context.Background()
	fileID := "file-abc-123"
	ownerID := "user-owner-1"
	nonOwnerID := "user-other-2"
	adminID := "user-admin-3"
	financeUserID := "user-finance-4"

	// Claims untuk setiap user
	ownerClaims := jwt.MapClaims{"sub": ownerID, "role": "user"}
	adminClaims := jwt.MapClaims{"sub": adminID, "role": "admin"}
	financeClaims := jwt.MapClaims{"sub": financeUserID, "role": "finance"}
	nonOwnerClaims := jwt.MapClaims{"sub": nonOwnerID, "role": "user"}

	// Metadata file yang akan diuji
	fileWithOwner := &model.FileMetadata{ID: fileID, OwnerUserID: &ownerID, Tags: []string{}}
	fileWithTags := &model.FileMetadata{ID: fileID, OwnerUserID: &ownerID, Tags: []string{"keuangan"}}

	testCases := []struct {
		name          string
		claims        jwt.MapClaims
		setupMock     func(mockRepo *MockFileRepository)
		expectError   bool
		expectedError error
	}{
		{
			name:   "Success - Owner can access file",
			claims: ownerClaims,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("GetByID", ctx, fileID).Return(fileWithOwner, nil).Once()
			},
			expectError: false,
		},
		{
			name:   "Success - Admin can access file",
			claims: adminClaims,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("GetByID", ctx, fileID).Return(fileWithOwner, nil).Once()
			},
			expectError: false,
		},
		{
			name:   "Success - Role with tag access",
			claims: financeClaims,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("GetByID", ctx, fileID).Return(fileWithTags, nil).Once()
				mockRepo.On("CheckRoleAccess", ctx, fileID, "finance").Return(true, nil).Once()
			},
			expectError: false,
		},
		{
			name:          "Failure - Non-owner cannot access untagged file",
			claims:        nonOwnerClaims,
			expectedError: ErrAccessDenied,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("GetByID", ctx, fileID).Return(fileWithOwner, nil).Once()
			},
			expectError: true,
		},
		{
			name:          "Failure - Role without tag access",
			claims:        nonOwnerClaims,
			expectedError: ErrAccessDenied,
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("GetByID", ctx, fileID).Return(fileWithTags, nil).Once()
				mockRepo.On("CheckRoleAccess", ctx, fileID, "user").Return(false, nil).Once()
			},
			expectError: true,
		},
		{
			name:          "Failure - File not found",
			claims:        ownerClaims,
			expectedError: errors.New("file not found"),
			setupMock: func(mockRepo *MockFileRepository) {
				mockRepo.On("GetByID", ctx, fileID).Return(nil, errors.New("file not found")).Once()
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := new(MockFileRepository)
			mockStore := new(MockStorage)
			tc.setupMock(mockRepo)

			// FIX: Gunakan config yang valid, meskipun tidak digunakan secara langsung di sini, ini adalah praktik yang baik.
			svc := NewFileService(mockRepo, mockStore, &fileserviceconfig.Config{}, nil)
			metadata, err := svc.GetFileMetadata(ctx, fileID, tc.claims)

			if tc.expectError {
				require.Error(t, err)
				assert.Equal(t, tc.expectedError, err)
				assert.Nil(t, metadata)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, metadata)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

// BARU: Tambahkan tes untuk GetFileReader
func TestFileService_GetFileReader(t *testing.T) {
	mockStore := new(MockStorage)
	svc := NewFileService(nil, mockStore, &fileserviceconfig.Config{}, nil)
	path := "test/file.txt"

	// Mock akan mengembalikan reader string dan tidak ada error
	mockReader := io.NopCloser(strings.NewReader("file content"))
	mockStore.On("Get", context.Background(), path).Return(mockReader, nil).Once()

	reader, err := svc.GetFileReader(context.Background(), path)
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Baca konten dari reader untuk memastikan itu benar
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "file content", string(content))

	mockStore.AssertExpectations(t)
}
