// file: internal/repository/file_repository_integration_test.go

//go:build integration
// +build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB sekarang mengembalikan DBTX (yang akan berupa pgx.Tx) dan fungsi teardown.
func setupTestDB(t *testing.T) (DBTX, func()) {
	databaseURL := os.Getenv("DATABASE_URL_TEST")
	if databaseURL == "" {
		t.Skip("Skipping integration test: DATABASE_URL_TEST is not set.")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	require.NoError(t, err, "Failed to connect to test database")

	createTableSQL := `
    CREATE TABLE IF NOT EXISTS files (
        id UUID PRIMARY KEY,
        original_name VARCHAR(255) NOT NULL,
        storage_path VARCHAR(255) NOT NULL,
        mime_type VARCHAR(100) NOT NULL,
        size_bytes BIGINT NOT NULL,
        owner_user_id VARCHAR(36),
        created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
    );`
	_, err = pool.Exec(context.Background(), createTableSQL)
	require.NoError(t, err, "Failed to create 'files' table")

	// Mulai transaksi untuk tes ini
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)

	// Fungsi teardown akan me-rollback transaksi, membersihkan semua data tes
	teardown := func() {
		err := tx.Rollback(context.Background())
		if err != nil && err != pgx.ErrTxClosed {
			t.Logf("Warning: failed to rollback transaction: %v", err)
		}
		pool.Close()
	}

	// Kembalikan transaksi (tx) sebagai DBTX
	return tx, teardown
}

func TestPostgresFileRepository_Integration(t *testing.T) {
	// Dapatkan transaksi sebagai DBTX dari helper
	db, teardown := setupTestDB(t)
	defer teardown()

	// Buat repository dengan transaksi tersebut. Ini sekarang valid.
	repo := NewPostgresFileRepository(db)
	ctx := context.Background()

	ownerID := uuid.New().String()
	metadata := &model.FileMetadata{
		ID:           uuid.New().String(),
		OriginalName: "invoice_2025.pdf",
		StoragePath:  "/storage/invoice_2025.pdf",
		MimeType:     "application/pdf",
		SizeBytes:    123456,
		OwnerUserID:  &ownerID,
	}

	// 1. Test Create
	err := repo.Create(ctx, metadata)
	require.NoError(t, err, "Create should not return an error")

	// 2. Test GetByID
	retrieved, err := repo.GetByID(ctx, metadata.ID)
	require.NoError(t, err, "GetByID should find the created record")
	require.NotNil(t, retrieved, "Retrieved metadata should not be nil")

	assert.Equal(t, metadata.ID, retrieved.ID)
	assert.Equal(t, metadata.OriginalName, retrieved.OriginalName)
	assert.Equal(t, metadata.SizeBytes, retrieved.SizeBytes)
	assert.Equal(t, *metadata.OwnerUserID, *retrieved.OwnerUserID)
	assert.WithinDuration(t, time.Now(), retrieved.CreatedAt, 2*time.Second)

	// 3. Test DeleteByID
	err = repo.DeleteByID(ctx, metadata.ID)
	require.NoError(t, err, "DeleteByID should not return an error")

	// 4. Verify Deletion
	_, err = repo.GetByID(ctx, metadata.ID)
	require.Error(t, err, "GetByID should return an error for a deleted record")
	assert.Equal(t, pgx.ErrNoRows, err, "The error should be pgx.ErrNoRows")
}
