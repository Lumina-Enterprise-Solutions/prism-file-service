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

// setupTestDB diubah untuk mengembalikan pool dan fungsi teardown yang lebih robust.
func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	databaseURL := os.Getenv("DATABASE_URL_TEST")
	if databaseURL == "" {
		t.Skip("Skipping integration test: DATABASE_URL_TEST is not set.")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	require.NoError(t, err, "Failed to connect to test database")

	// Skema sederhana untuk tes file repository
	createTablesSQL := `
    DROP TABLE IF EXISTS file_tags, file_access_rules, files CASCADE;
    CREATE TABLE IF NOT EXISTS files (
        id UUID PRIMARY KEY,
        original_name VARCHAR(255) NOT NULL,
        storage_path VARCHAR(255) NOT NULL,
        mime_type VARCHAR(100) NOT NULL,
        size_bytes BIGINT NOT NULL,
        owner_user_id VARCHAR(36),
        created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        deleted_at TIMESTAMPTZ
    );
    CREATE TABLE IF NOT EXISTS file_tags (
        file_id UUID NOT NULL REFERENCES files(id) ON DELETE CASCADE,
        tag_name VARCHAR(100) NOT NULL,
        PRIMARY KEY (file_id, tag_name)
    );
    CREATE TABLE IF NOT EXISTS file_access_rules (
        tag_name VARCHAR(100) NOT NULL,
        role_name VARCHAR(100) NOT NULL,
        PRIMARY KEY (tag_name, role_name)
    );`
	_, err = pool.Exec(context.Background(), createTablesSQL)
	require.NoError(t, err, "Failed to create test tables")

	teardown := func() {
		// Bersihkan tabel setelah tes selesai
		_, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS file_tags, file_access_rules, files CASCADE;")
		if err != nil {
			t.Logf("Warning: failed to drop tables on teardown: %v", err)
		}
		pool.Close()
	}

	return pool, teardown
}

func TestPostgresFileRepository_Integration(t *testing.T) {
	// FIX: Dapatkan pool, bukan transaksi
	dbpool, teardown := setupTestDB(t)
	defer teardown()

	// FIX: Inisialisasi repo dengan pool
	repo := NewPostgresFileRepository(dbpool)
	ctx := context.Background()

	ownerID := uuid.New().String()
	tags := []string{"invoice", "q1_2025"}
	metadata := &model.FileMetadata{
		ID:           uuid.New().String(),
		OriginalName: "invoice_2025.pdf",
		StoragePath:  "/storage/invoice_2025.pdf",
		MimeType:     "application/pdf",
		SizeBytes:    123456,
		OwnerUserID:  &ownerID,
	}

	// 1. Test Create
	// FIX: Panggil Create dengan argumen tags
	err := repo.Create(ctx, metadata, tags)
	require.NoError(t, err, "Create should not return an error")

	// 2. Test GetByID
	retrieved, err := repo.GetByID(ctx, metadata.ID)
	require.NoError(t, err, "GetByID should find the created record")
	require.NotNil(t, retrieved, "Retrieved metadata should not be nil")

	assert.Equal(t, metadata.ID, retrieved.ID)
	assert.Equal(t, metadata.OriginalName, retrieved.OriginalName)
	assert.Equal(t, metadata.SizeBytes, retrieved.SizeBytes)
	assert.Equal(t, *metadata.OwnerUserID, *retrieved.OwnerUserID)
	assert.ElementsMatch(t, tags, retrieved.Tags, "Tags should match")
	assert.WithinDuration(t, time.Now(), retrieved.CreatedAt, 2*time.Second)

	// 3. Test CheckRoleAccess (kasus gagal)
	hasAccess, err := repo.CheckRoleAccess(ctx, metadata.ID, "finance")
	require.NoError(t, err)
	assert.False(t, hasAccess, "Role 'finance' seharusnya tidak memiliki akses")

	// Setup aturan akses untuk kasus berhasil
	_, err = dbpool.Exec(ctx, "INSERT INTO file_access_rules (tag_name, role_name) VALUES ('invoice', 'finance')")
	require.NoError(t, err)

	// 4. Test CheckRoleAccess (kasus berhasil)
	hasAccess, err = repo.CheckRoleAccess(ctx, metadata.ID, "finance")
	require.NoError(t, err)
	assert.True(t, hasAccess, "Role 'finance' sekarang seharusnya memiliki akses")

	// 5. Test DeleteByID
	err = repo.DeleteByID(ctx, metadata.ID)
	require.NoError(t, err, "DeleteByID should not return an error")

	// 6. Verify Deletion
	_, err = repo.GetByID(ctx, metadata.ID)
	require.Error(t, err, "GetByID should return an error for a deleted record")
	assert.ErrorIs(t, err, pgx.ErrNoRows, "The error should be pgx.ErrNoRows")
}
