package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type Store struct {
	pool *pgxpool.Pool
}

type AnchorInfo struct {
	ProjectID string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ExistingRecord struct {
	ID       string
	FileHash string
	Version  int
}

type SearchParams struct {
	ProjectID      string
	DocTypes       []string
	KnowledgeTypes []string
	Limit          int
	MustLatest     bool
	OrderBy        string
	Since          *time.Time
}

func NewStore(databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) EnsureSchema(ctx context.Context, dimension int) error {
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return err
	}

	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS knowledge (
  id VARCHAR(32) PRIMARY KEY,
  knowledge_type VARCHAR(16) NOT NULL,
  doc_type VARCHAR(32),
  insight_type VARCHAR(32),
  source_type VARCHAR(16),
  raw_content_path VARCHAR(1024),
  project_id VARCHAR(64) NOT NULL,
  project_name VARCHAR(128),
  machine_id VARCHAR(32),
  file_path VARCHAR(1024),
  relative_path VARCHAR(512),
  file_hash VARCHAR(64),
  title VARCHAR(256),
  content TEXT NOT NULL,
  summary TEXT,
  structured_content JSONB,
  category_l1 VARCHAR(64),
  category_l2 VARCHAR(64),
  category_l3 VARCHAR(64),
  tags JSONB,
  embedding VECTOR(%d),
  related_ids JSONB,
  version INT,
  is_latest BOOLEAN,
  superseded_by VARCHAR(32),
  supersede_reason VARCHAR(32),
  status VARCHAR(16),
  decay_rule VARCHAR(32),
  expires_at TIMESTAMPTZ,
  is_high_value BOOLEAN,
  reproducible BOOLEAN,
  applicable_to JSONB,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ
);`, dimension)
	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS knowledge_project_id_idx ON knowledge (project_id)",
		"CREATE INDEX IF NOT EXISTS knowledge_relative_path_idx ON knowledge (relative_path)",
		"CREATE INDEX IF NOT EXISTS knowledge_doc_type_idx ON knowledge (doc_type)",
		"CREATE INDEX IF NOT EXISTS knowledge_knowledge_type_idx ON knowledge (knowledge_type)",
		"CREATE INDEX IF NOT EXISTS knowledge_is_latest_idx ON knowledge (is_latest)",
		"CREATE INDEX IF NOT EXISTS knowledge_updated_at_idx ON knowledge (updated_at)",
	}
	for _, stmt := range indexes {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

func (s *Store) FindLatestByRelativePath(ctx context.Context, projectID, relativePath string) (*ExistingRecord, error) {
	query := `SELECT id, file_hash, version FROM knowledge WHERE project_id = $1 AND relative_path = $2 AND is_latest = true LIMIT 1`
	row := s.pool.QueryRow(ctx, query, projectID, relativePath)
	var record ExistingRecord
	if err := row.Scan(&record.ID, &record.FileHash, &record.Version); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *Store) SaveKnowledgeBlock(ctx context.Context, block *KnowledgeBlock) error {
	query := `
INSERT INTO knowledge (
	id, knowledge_type, doc_type, insight_type, source_type, raw_content_path,
	project_id, project_name, machine_id, file_path, relative_path, file_hash,
	title, content, summary, structured_content, category_l1, category_l2, category_l3,
	tags, embedding, related_ids, version, is_latest, superseded_by, supersede_reason,
	status, decay_rule, expires_at, is_high_value, reproducible, applicable_to,
	created_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19,
	$20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34
)`
	structured, _ := json.Marshal(block.StructuredContent)
	tags, _ := json.Marshal(block.Tags)
	related, _ := json.Marshal(block.RelatedIDs)
	applicable, _ := json.Marshal(block.ApplicableTo)

	if block.CreatedAt.IsZero() {
		block.CreatedAt = time.Now().UTC()
	}
	if block.UpdatedAt.IsZero() {
		block.UpdatedAt = block.CreatedAt
	}

	_, err := s.pool.Exec(ctx, query,
		block.ID, block.KnowledgeType, block.DocType, block.InsightType, block.SourceType, block.RawContentPath,
		block.ProjectID, block.ProjectName, block.MachineID, block.FilePath, block.RelativePath, block.FileHash,
		block.Title, block.Content, block.Summary, structured, block.CategoryL1, block.CategoryL2, block.CategoryL3,
		tags, block.Embedding, related, block.Version, block.IsLatest, block.SupersededBy, block.SupersedeReason,
		block.Status, block.DecayRule, block.ExpiresAt, block.IsHighValue, block.Reproducible, applicable,
		block.CreatedAt, block.UpdatedAt,
	)
	return err
}

func (s *Store) DeprecateBlock(ctx context.Context, id, supersededBy, reason string) error {
	query := `
UPDATE knowledge
SET is_latest = false, superseded_by = $2, supersede_reason = $3, status = 'deprecated', updated_at = $4
WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, id, supersededBy, reason, time.Now().UTC())
	return err
}

func (s *Store) DeleteBlock(ctx context.Context, tx pgx.Tx, id string) error {
	// 支持事务内的删除
	query := `DELETE FROM knowledge WHERE id = $1`
	var err error
	if tx != nil {
		_, err = tx.Exec(ctx, query, id)
	} else {
		_, err = s.pool.Exec(ctx, query, id)
	}
	return err
}

func (s *Store) FetchObservations(ctx context.Context, ids []string) ([]map[string]any, error) {
	query := `
SELECT id,
       COALESCE(title, '') as title,
       COALESCE(file_path, '') as file_path,
       COALESCE(relative_path, '') as relative_path,
       project_id,
       COALESCE(knowledge_type, '') as knowledge_type,
       COALESCE(doc_type, '') as doc_type,
       COALESCE(insight_type, '') as insight_type,
       COALESCE(source_type, '') as source_type,
       COALESCE(summary, '') as summary,
       content,
       COALESCE(structured_content, '{}'::jsonb) as structured_content,
       COALESCE(tags, '[]'::jsonb) as tags,
       COALESCE(related_ids, '[]'::jsonb) as related_ids,
       created_at,
       updated_at
FROM knowledge
WHERE id = ANY($1)`

	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		row, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) FetchAnchor(ctx context.Context, anchorID string) (*AnchorInfo, error) {
	query := `SELECT project_id, created_at, COALESCE(updated_at, created_at) FROM knowledge WHERE id = $1`
	row := s.pool.QueryRow(ctx, query, anchorID)
	var anchor AnchorInfo
	if err := row.Scan(&anchor.ProjectID, &anchor.CreatedAt, &anchor.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &anchor, nil
}

func (s *Store) FetchTimeline(ctx context.Context, projectID string, start, end time.Time, limit int) ([]map[string]any, error) {
	query := `
SELECT id,
       COALESCE(title, '') as title,
       COALESCE(summary, '') as summary,
       COALESCE(doc_type, '') as doc_type,
       COALESCE(knowledge_type, '') as knowledge_type,
       COALESCE(relative_path, '') as relative_path,
       updated_at,
       created_at
FROM knowledge
WHERE project_id = $1 AND COALESCE(updated_at, created_at) >= $2 AND COALESCE(updated_at, created_at) <= $3
ORDER BY COALESCE(updated_at, created_at) ASC
LIMIT $4`

	rows, err := s.pool.Query(ctx, query, projectID, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		row, err := scanTimelineRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) SearchByKeyword(ctx context.Context, projectID, keyword string, limit int) ([]map[string]any, error) {
	query := `
SELECT id, title, summary, content
FROM knowledge
WHERE project_id = $1 AND is_latest = true
  AND (title ILIKE $2 OR content ILIKE $2 OR summary ILIKE $2)
LIMIT $3`
	like := fmt.Sprintf("%%%s%%", keyword)
	rows, err := s.pool.Query(ctx, query, projectID, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, title, summary, content string
		if err := rows.Scan(&id, &title, &summary, &content); err != nil {
			return nil, err
		}
		results = append(results, map[string]any{
			"id":      id,
			"title":   title,
			"summary": summary,
			"content": content,
		})
	}
	return results, rows.Err()
}

func (s *Store) SearchVector(ctx context.Context, embedding pgvector.Vector, params SearchParams) ([]SearchRow, error) {
	query := `
SELECT id,
       COALESCE(title, '') as title,
       COALESCE(file_path, '') as file_path,
       COALESCE(summary, '') as summary,
       content,
       COALESCE(doc_type, '') as doc_type,
       COALESCE(knowledge_type, '') as knowledge_type,
       project_id,
       (embedding <=> $1) AS distance
FROM knowledge`

	conditions := []string{}
	args := []any{embedding}
	argIndex := 2

	if params.MustLatest {
		conditions = append(conditions, "is_latest = true")
	}
	if params.ProjectID != "" {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIndex))
		args = append(args, params.ProjectID)
		argIndex++
	}
	if len(params.DocTypes) > 0 {
		conditions = append(conditions, fmt.Sprintf("doc_type = ANY($%d)", argIndex))
		args = append(args, params.DocTypes)
		argIndex++
	}
	if len(params.KnowledgeTypes) > 0 {
		conditions = append(conditions, fmt.Sprintf("knowledge_type = ANY($%d)", argIndex))
		args = append(args, params.KnowledgeTypes)
		argIndex++
	}
	if params.Since != nil {
		conditions = append(conditions, fmt.Sprintf("COALESCE(updated_at, created_at) >= $%d", argIndex))
		args = append(args, *params.Since)
		argIndex++
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if params.OrderBy == "time_desc" {
		query += " ORDER BY COALESCE(updated_at, created_at) DESC"
	} else {
		query += " ORDER BY embedding <=> $1"
	}

	query += fmt.Sprintf(" LIMIT $%d", argIndex)
	args = append(args, params.Limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchRow
	for rows.Next() {
		var row SearchRow
		var distance float64
		if err := rows.Scan(&row.ID, &row.Title, &row.FilePath, &row.Summary, &row.Content, &row.DocType, &row.KnowledgeType, &row.ProjectID, &distance); err != nil {
			return nil, err
		}
		row.Score = 1 - distance
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) SearchSimilar(ctx context.Context, embedding pgvector.Vector, projectID string, docType string, limit int) ([]map[string]any, error) {
	query := `
SELECT id, content, doc_type, (embedding <=> $1) AS distance
FROM knowledge
WHERE is_latest = true AND project_id = $2
`
	args := []any{embedding, projectID}
	if docType != "" {
		query += " AND doc_type = $3"
		args = append(args, docType)
	}
	query += " ORDER BY embedding <=> $1 LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, content, docTypeValue string
		var distance float64
		if err := rows.Scan(&id, &content, &docTypeValue, &distance); err != nil {
			return nil, err
		}
		results = append(results, map[string]any{
			"id":         id,
			"content":    content,
			"doc_type":   docTypeValue,
			"distance":   distance,
			"similarity": 1 - distance,
		})
	}
	return results, rows.Err()
}

func scanTimelineRow(rows pgx.Rows) (map[string]any, error) {
	var (
		id, title, summary, docType, knowledgeType, relativePath string
		updatedAt, createdAt                                     time.Time
	)
	if err := rows.Scan(&id, &title, &summary, &docType, &knowledgeType, &relativePath, &updatedAt, &createdAt); err != nil {
		return nil, err
	}
	stamp := updatedAt
	if stamp.IsZero() {
		stamp = createdAt
	}
	return map[string]any{
		"id":             id,
		"title":          title,
		"summary":        summary,
		"doc_type":       docType,
		"knowledge_type": knowledgeType,
		"relative_path":  relativePath,
		"updated_at":     formatTime(stamp),
	}, nil
}

func scanObservation(rows pgx.Rows) (map[string]any, error) {
	var (
		id, title, filePath, relativePath, projectID, knowledgeType, docType, insightType, sourceType string
		summary, content                                                                              string
		structuredRaw, tagsRaw, relatedRaw                                                            []byte
		createdAt, updatedAt                                                                          time.Time
	)
	if err := rows.Scan(
		&id,
		&title,
		&filePath,
		&relativePath,
		&projectID,
		&knowledgeType,
		&docType,
		&insightType,
		&sourceType,
		&summary,
		&content,
		&structuredRaw,
		&tagsRaw,
		&relatedRaw,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	structured := decodeJSON(structuredRaw)
	tags := decodeJSON(tagsRaw)
	related := decodeJSON(relatedRaw)

	return map[string]any{
		"id":                 id,
		"title":              title,
		"file_path":          filePath,
		"relative_path":      relativePath,
		"project_id":         projectID,
		"knowledge_type":     knowledgeType,
		"doc_type":           docType,
		"insight_type":       insightType,
		"source_type":        sourceType,
		"summary":            summary,
		"content":            content,
		"structured_content": structured,
		"tags":               tags,
		"related_ids":        related,
		"created_at":         formatTime(createdAt),
		"updated_at":         formatTime(updatedAt),
	}, nil
}
