package repository

import (
	"context"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FileRepository interface {
	Create(ctx context.Context, metadata *model.FileMetadata) error
	GetByID(ctx context.Context, id string) (*model.FileMetadata, error)
}

type postgresFileRepository struct {
	db *pgxpool.Pool
}

func NewPostgresFileRepository(db *pgxpool.Pool) FileRepository {
	return &postgresFileRepository{db: db}
}

func (r *postgresFileRepository) Create(ctx context.Context, metadata *model.FileMetadata) error {
	sql := `INSERT INTO files (id, original_name, storage_path, mime_type, size_bytes, owner_user_id)
            VALUES ($1, $2, $3, $4, $5, $6);`

	_, err := r.db.Exec(ctx, sql,
		metadata.ID,
		metadata.OriginalName,
		metadata.StoragePath,
		metadata.MimeType,
		metadata.SizeBytes,
		metadata.OwnerUserID,
	)
	return err
}

func (r *postgresFileRepository) GetByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	var metadata model.FileMetadata
	sql := `SELECT id, original_name, storage_path, mime_type, size_bytes, owner_user_id, created_at
            FROM files WHERE id = $1;`

	err := r.db.QueryRow(ctx, sql, id).Scan(
		&metadata.ID,
		&metadata.OriginalName,
		&metadata.StoragePath,
		&metadata.MimeType,
		&metadata.SizeBytes,
		&metadata.OwnerUserID,
		&metadata.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &metadata, nil
}
