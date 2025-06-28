package repository

import (
	"context"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type FileRepository interface {
	Create(ctx context.Context, metadata *model.FileMetadata, tags []string) error
	GetByID(ctx context.Context, id string) (*model.FileMetadata, error)
	DeleteByID(ctx context.Context, id string) error
	CheckRoleAccess(ctx context.Context, fileID string, roleName string) (bool, error)
}

type postgresFileRepository struct {
	db *pgxpool.Pool
}

func NewPostgresFileRepository(db *pgxpool.Pool) FileRepository {
	return &postgresFileRepository{db: db}
}

func (r *postgresFileRepository) Create(ctx context.Context, metadata *model.FileMetadata, tags []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	sqlInsertFile := `INSERT INTO files (id, original_name, storage_path, mime_type, size_bytes, owner_user_id)
                      VALUES ($1, $2, $3, $4, $5, $6);`
	_, err = tx.Exec(ctx, sqlInsertFile, metadata.ID, metadata.OriginalName, metadata.StoragePath, metadata.MimeType, metadata.SizeBytes, metadata.OwnerUserID)
	if err != nil {
		return err
	}

	if len(tags) > 0 {
		batch := &pgx.Batch{}
		sqlInsertTags := `INSERT INTO file_tags (file_id, tag_name) VALUES ($1, $2);`
		for _, tag := range tags {
			batch.Queue(sqlInsertTags, metadata.ID, tag)
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *postgresFileRepository) GetByID(ctx context.Context, id string) (*model.FileMetadata, error) {
	var metadata model.FileMetadata
	sql := `SELECT f.id, f.original_name, f.storage_path, f.mime_type, f.size_bytes, f.owner_user_id, f.created_at,
             COALESCE(array_agg(ft.tag_name) FILTER (WHERE ft.tag_name IS NOT NULL), '{}') as tags
            FROM files f
            LEFT JOIN file_tags ft ON f.id = ft.file_id
            WHERE f.id = $1 AND f.deleted_at IS NULL
            GROUP BY f.id;`

	err := r.db.QueryRow(ctx, sql, id).Scan(
		&metadata.ID, &metadata.OriginalName, &metadata.StoragePath, &metadata.MimeType,
		&metadata.SizeBytes, &metadata.OwnerUserID, &metadata.CreatedAt, &metadata.Tags,
	)
	if err != nil {
		return nil, err
	}
	return &metadata, nil
}

func (r *postgresFileRepository) DeleteByID(ctx context.Context, id string) error {
	sql := `DELETE FROM files WHERE id = $1;`
	_, err := r.db.Exec(ctx, sql, id)
	return err
}

func (r *postgresFileRepository) CheckRoleAccess(ctx context.Context, fileID string, roleName string) (bool, error) {
	var hasAccess bool
	sql := `SELECT EXISTS (
                SELECT 1
                FROM file_tags ft
                JOIN file_access_rules far ON ft.tag_name = far.tag_name
                WHERE ft.file_id = $1 AND far.role_name = $2
            );`
	err := r.db.QueryRow(ctx, sql, fileID, roleName).Scan(&hasAccess)
	if err != nil {
		return false, err
	}
	return hasAccess, nil
}
