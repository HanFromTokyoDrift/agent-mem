package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type Store struct {
	pool *pgxpool.Pool
}

type ProjectRecord struct {
	ID          string
	ProjectName string
	ProjectKey  string
	OwnerID     string
}

type TimelineRecord struct {
	ID          string
	ContentType string
	Summary     string
	Ts          int64
}

type MemoryRow struct {
	ID          string
	ContentType string
	Content     string
	Summary     string
	Tags        []string
	Axes        MemoryAxes
	IndexPath   []string
	Ts          int64
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

func (s *Store) EnsureSchema(ctx context.Context, dimension int, reset bool) error {
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto"); err != nil {
		return err
	}
	if reset {
		cleanup := `
DROP TABLE IF EXISTS fragments CASCADE;
DROP TABLE IF EXISTS memories CASCADE;
DROP TABLE IF EXISTS projects CASCADE;
DROP TABLE IF EXISTS knowledge CASCADE;`
		if _, err := s.pool.Exec(ctx, cleanup); err != nil {
			return err
		}
	}

	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id TEXT NOT NULL,
  project_key TEXT NOT NULL,
  project_name TEXT NOT NULL,
  machine_name TEXT,
  project_path TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(owner_id, project_key)
);

CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT,
  ts BIGINT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  summary TEXT,
  tags JSONB,
  axes JSONB,
  index_path JSONB,
  chunk_count INT DEFAULT 1,
  embedding_done BOOLEAN DEFAULT false,
  avg_embedding VECTOR(%[1]d)
);

CREATE TABLE IF NOT EXISTS fragments (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  chunk_index INT NOT NULL,
  content TEXT NOT NULL,
  embedding VECTOR(%[1]d),
  ts TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(memory_id, chunk_index)
);

CREATE TABLE IF NOT EXISTS memory_versions (
  id BIGSERIAL PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT,
  ts BIGINT NOT NULL,
  summary TEXT,
  tags JSONB,
  axes JSONB,
  index_path JSONB,
  chunk_count INT DEFAULT 1,
  avg_embedding VECTOR(%[1]d),
  created_at TIMESTAMPTZ,
  replaced_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS memory_arbitrations (
  id BIGSERIAL PRIMARY KEY,
  owner_id TEXT NOT NULL,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  candidate_memory_id TEXT,
  new_memory_id TEXT,
  action TEXT NOT NULL,
  similarity DOUBLE PRECISION,
  old_summary TEXT,
  new_summary TEXT,
  model TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
`, dimension)

	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return err
	}

	// 迁移：为已有表添加新字段（幂等操作）
	migrations := []string{
		// projects 表新增 owner_id 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='owner_id') THEN
				ALTER TABLE projects ADD COLUMN owner_id TEXT;
			END IF;
		END $$`,
		// projects 表新增 project_key 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='project_key') THEN
				ALTER TABLE projects ADD COLUMN project_key TEXT;
			END IF;
		END $$`,
		// projects 表 machine_name 改为可空
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='machine_name' AND is_nullable='NO') THEN
				ALTER TABLE projects ALTER COLUMN machine_name DROP NOT NULL;
			END IF;
		END $$`,
		// projects 表 project_path 改为可空
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='project_path' AND is_nullable='NO') THEN
				ALTER TABLE projects ALTER COLUMN project_path DROP NOT NULL;
			END IF;
		END $$`,
		// 移除旧的唯一约束（machine_name, project_path）
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='projects_machine_name_project_path_key') THEN
				ALTER TABLE projects DROP CONSTRAINT projects_machine_name_project_path_key;
			END IF;
		END $$`,
		// memories 表添加 avg_embedding 字段
		fmt.Sprintf(`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='avg_embedding') THEN
				ALTER TABLE memories ADD COLUMN avg_embedding VECTOR(%d);
			END IF;
		END $$`, dimension),
		// memories 表添加 updated_at 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='updated_at') THEN
				ALTER TABLE memories ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW();
			END IF;
		END $$`,
		// memories 表添加 summary 字段（如果旧版本缺失）
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='summary') THEN
				ALTER TABLE memories ADD COLUMN summary TEXT;
			END IF;
		END $$`,
		// memories 表添加 tags 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='tags') THEN
				ALTER TABLE memories ADD COLUMN tags JSONB;
			END IF;
		END $$`,
		// memories 表添加 axes 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='axes') THEN
				ALTER TABLE memories ADD COLUMN axes JSONB;
			END IF;
		END $$`,
		// memories 表添加 index_path 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='index_path') THEN
				ALTER TABLE memories ADD COLUMN index_path JSONB;
			END IF;
		END $$`,
		// memory_versions 表添加 axes 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memory_versions' AND column_name='axes') THEN
				ALTER TABLE memory_versions ADD COLUMN axes JSONB;
			END IF;
		END $$`,
		// memory_versions 表添加 index_path 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memory_versions' AND column_name='index_path') THEN
				ALTER TABLE memory_versions ADD COLUMN index_path JSONB;
			END IF;
		END $$`,
	}
	for _, stmt := range migrations {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("迁移失败: %w", err)
		}
	}

	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_owner_key ON projects(owner_id, project_key)",
		"CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id)",
		"CREATE INDEX IF NOT EXISTS idx_projects_machine ON projects(machine_name)",
		"CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(project_path)",
		"CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(project_name)",
		"CREATE INDEX IF NOT EXISTS idx_projects_key ON projects(project_key)",
		"CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(content_type)",
		"CREATE INDEX IF NOT EXISTS idx_memories_ts ON memories(ts DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(content_hash)",
		"CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_tags_gin ON memories USING GIN (tags)",
		"CREATE INDEX IF NOT EXISTS idx_memories_axes_gin ON memories USING GIN (axes)",
		"CREATE INDEX IF NOT EXISTS idx_memories_index_path_gin ON memories USING GIN (index_path)",
		"CREATE INDEX IF NOT EXISTS idx_memories_index_path_l1 ON memories ((index_path->>0)) WHERE index_path IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_memories_index_path_l2 ON memories ((index_path->>1)) WHERE index_path IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_memories_index_path_l3 ON memories ((index_path->>2)) WHERE index_path IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_memories_avg_embedding ON memories USING hnsw (avg_embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS idx_fragments_memory ON fragments(memory_id)",
		"CREATE INDEX IF NOT EXISTS idx_fragments_embedding ON fragments USING hnsw (embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS idx_fragments_fts ON fragments USING GIN (to_tsvector('simple', content))",
		"CREATE INDEX IF NOT EXISTS idx_memory_versions_memory ON memory_versions(memory_id)",
		"CREATE INDEX IF NOT EXISTS idx_memory_versions_project ON memory_versions(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_memory_arbitrations_project ON memory_arbitrations(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_memory_arbitrations_owner ON memory_arbitrations(owner_id)",
	}
	for _, stmt := range indexes {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertProject(ctx context.Context, ownerID, projectKey, projectName, machineName, projectPath string) (ProjectRecord, error) {
	query := `
INSERT INTO projects (owner_id, project_key, project_name, machine_name, project_path)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (owner_id, project_key)
DO UPDATE SET project_name = EXCLUDED.project_name,
              machine_name = CASE WHEN EXCLUDED.machine_name IS NULL OR EXCLUDED.machine_name = '' THEN projects.machine_name ELSE EXCLUDED.machine_name END,
              project_path = CASE WHEN EXCLUDED.project_path IS NULL OR EXCLUDED.project_path = '' THEN projects.project_path ELSE EXCLUDED.project_path END,
              updated_at = NOW()
RETURNING id, project_name, project_key, owner_id`

	var (
		projectID   string
		storedName  string
		storedKey   string
		storedOwner string
	)
	if err := s.pool.QueryRow(ctx, query, ownerID, projectKey, projectName, nullableString(machineName), nullableString(projectPath)).Scan(&projectID, &storedName, &storedKey, &storedOwner); err != nil {
		return ProjectRecord{}, err
	}
	return ProjectRecord{ID: projectID, ProjectName: storedName, ProjectKey: storedKey, OwnerID: storedOwner}, nil
}

func (s *Store) FindProjectIDByKey(ctx context.Context, ownerID, projectKey string) (string, error) {
	query := `SELECT id FROM projects WHERE owner_id = $1 AND project_key = $2`
	var id string
	if err := s.pool.QueryRow(ctx, query, ownerID, projectKey).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func (s *Store) BackfillProjectIdentity(ctx context.Context, ownerID string) error {
	owner := strings.TrimSpace(ownerID)
	if owner == "" {
		owner = defaultOwnerID
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE projects
SET owner_id = $1
WHERE owner_id IS NULL OR owner_id = ''`, owner); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE projects
SET project_key = COALESCE(NULLIF(project_key, ''), NULLIF(project_path, ''), project_name)
WHERE project_key IS NULL OR project_key = ''`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE projects
SET project_name = COALESCE(NULLIF(project_name, ''), project_key)
WHERE project_name IS NULL OR project_name = ''`); err != nil {
		return err
	}
	return nil
}

func (s *Store) FindDuplicateMemory(ctx context.Context, projectID, contentHash string, sinceTs int64) (string, error) {
	query := `SELECT id FROM memories WHERE project_id = $1 AND content_hash = $2 AND ts >= $3 LIMIT 1`
	var id string
	if err := s.pool.QueryRow(ctx, query, projectID, contentHash, sinceTs).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func (s *Store) UpdateMemoryTimestamp(ctx context.Context, memoryID string, ts int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE memories SET ts = $2, updated_at = NOW() WHERE id = $1`, memoryID, ts)
	return err
}

func (s *Store) InsertMemory(ctx context.Context, memory MemoryInsert) error {
	tagsJSON, _ := json.Marshal(memory.Tags)
	axesJSON, _ := json.Marshal(memory.Axes)
	indexPathJSON, _ := json.Marshal(memory.IndexPath)
	var avgVec any
	if len(memory.AvgEmbedding) > 0 {
		avgVec = pgvector.NewVector(memory.AvgEmbedding)
	}
	axesValue := nullableJSON(axesJSON, axesEmpty(memory.Axes))
	indexPathValue := nullableJSON(indexPathJSON, len(memory.IndexPath) == 0)
	_, err := s.pool.Exec(ctx, `
INSERT INTO memories (
  id, project_id, content_type, content, content_hash, ts,
  summary, tags, axes, index_path, chunk_count, embedding_done, avg_embedding
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11,$12,$13)`,
		memory.ID,
		memory.ProjectID,
		memory.ContentType,
		memory.Content,
		memory.ContentHash,
		memory.Ts,
		nullableString(memory.Summary),
		string(tagsJSON),
		axesValue,
		indexPathValue,
		memory.ChunkCount,
		memory.Embedded,
		avgVec,
	)
	return err
}

func (s *Store) InsertFragments(ctx context.Context, fragments []FragmentInsert) error {
	if len(fragments) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	query := `
INSERT INTO fragments (id, memory_id, chunk_index, content, embedding)
VALUES ($1,$2,$3,$4,$5)`
	for _, frag := range fragments {
		batch.Queue(query, frag.ID, frag.MemoryID, frag.ChunkIndex, frag.Content, pgvector.NewVector(frag.Embedding))
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range fragments {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) FetchMemories(ctx context.Context, ids []string) ([]MemoryRow, error) {
	if len(ids) == 0 {
		return []MemoryRow{}, nil
	}
	query := `
SELECT id, content_type, content, COALESCE(summary, ''), COALESCE(tags, '[]'::jsonb), COALESCE(axes, '{}'::jsonb), COALESCE(index_path, '[]'::jsonb), ts
FROM memories
WHERE id = ANY($1)`
	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRow
	for rows.Next() {
		var (
			row      MemoryRow
			tagsJSON []byte
			axesJSON []byte
			pathJSON []byte
		)
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Content, &row.Summary, &tagsJSON, &axesJSON, &pathJSON, &row.Ts); err != nil {
			return nil, err
		}
		row.Tags = decodeTags(tagsJSON)
		row.Axes = decodeAxes(axesJSON)
		row.IndexPath = decodeIndexPath(pathJSON)
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) FetchMemorySnapshot(ctx context.Context, memoryID string) (MemorySnapshot, error) {
	query := `
SELECT id,
       project_id,
       content_type,
       content,
       content_hash,
       ts,
       COALESCE(summary, ''),
       COALESCE(tags, '[]'::jsonb),
       COALESCE(axes, '{}'::jsonb),
       COALESCE(index_path, '[]'::jsonb),
       chunk_count,
       COALESCE(avg_embedding::text, ''),
       created_at
FROM memories
WHERE id = $1`
	var (
		row      MemorySnapshot
		tagsJSON []byte
		axesJSON []byte
		pathJSON []byte
		avgText  string
	)
	if err := s.pool.QueryRow(ctx, query, memoryID).Scan(
		&row.ID,
		&row.ProjectID,
		&row.ContentType,
		&row.Content,
		&row.ContentHash,
		&row.Ts,
		&row.Summary,
		&tagsJSON,
		&axesJSON,
		&pathJSON,
		&row.ChunkCount,
		&avgText,
		&row.CreatedAt,
	); err != nil {
		return MemorySnapshot{}, err
	}
	row.Tags = decodeTags(tagsJSON)
	row.Axes = decodeAxes(axesJSON)
	row.IndexPath = decodeIndexPath(pathJSON)
	if strings.TrimSpace(avgText) != "" {
		var vec pgvector.Vector
		if err := vec.Parse(avgText); err == nil {
			row.AvgEmbedding = vec.Slice()
		}
	}
	return row, nil
}

func (s *Store) InsertMemoryVersion(ctx context.Context, version MemoryVersionInsert) error {
	tagsJSON, _ := json.Marshal(version.Tags)
	axesJSON, _ := json.Marshal(version.Axes)
	indexPathJSON, _ := json.Marshal(version.IndexPath)
	var avgVec any
	if len(version.AvgEmbedding) > 0 {
		avgVec = pgvector.NewVector(version.AvgEmbedding)
	}
	axesValue := nullableJSON(axesJSON, axesEmpty(version.Axes))
	indexPathValue := nullableJSON(indexPathJSON, len(version.IndexPath) == 0)
	_, err := s.pool.Exec(ctx, `
INSERT INTO memory_versions (
  memory_id, project_id, content_type, content, content_hash, ts,
  summary, tags, axes, index_path, chunk_count, avg_embedding, created_at, replaced_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11,$12,$13,$14)`,
		version.MemoryID,
		version.ProjectID,
		version.ContentType,
		version.Content,
		version.ContentHash,
		version.Ts,
		nullableString(version.Summary),
		string(tagsJSON),
		axesValue,
		indexPathValue,
		version.ChunkCount,
		avgVec,
		version.CreatedAt,
		version.ReplacedAt,
	)
	return err
}

func (s *Store) InsertArbitrationLog(ctx context.Context, log ArbitrationLogInsert) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO memory_arbitrations (
  owner_id, project_id, candidate_memory_id, new_memory_id,
  action, similarity, old_summary, new_summary, model, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		log.OwnerID,
		log.ProjectID,
		nullableString(log.CandidateMemoryID),
		nullableString(log.NewMemoryID),
		log.Action,
		log.Similarity,
		nullableString(log.OldSummary),
		nullableString(log.NewSummary),
		nullableString(log.Model),
		log.CreatedAt,
	)
	return err
}

func (s *Store) FetchTimeline(ctx context.Context, projectID string, sinceTs int64, limit int) ([]TimelineRecord, error) {
	query := `
SELECT id, content_type, COALESCE(summary, ''), ts
FROM memories
WHERE project_id = $1 AND ts >= $2
ORDER BY ts DESC
LIMIT $3`
	rows, err := s.pool.Query(ctx, query, projectID, sinceTs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []TimelineRecord
	for rows.Next() {
		var row TimelineRecord
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Summary, &row.Ts); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) FetchTimelineByOwner(ctx context.Context, ownerID string, sinceTs int64, limit int) ([]TimelineRecord, error) {
	query := `
SELECT m.id, m.content_type, COALESCE(m.summary, ''), m.ts
FROM memories m
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $1 AND m.ts >= $2
ORDER BY m.ts DESC
LIMIT $3`
	rows, err := s.pool.Query(ctx, query, ownerID, sinceTs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []TimelineRecord
	for rows.Next() {
		var row TimelineRecord
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Summary, &row.Ts); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) ListProjects(ctx context.Context, ownerID string, limit int) ([]ProjectListItem, error) {
	query := `
SELECT p.owner_id,
       p.project_key,
       p.machine_name,
       p.project_path,
       p.project_name,
       COUNT(m.id) as memory_count,
       COALESCE(MAX(m.ts), 0) as latest_ts
FROM projects p
LEFT JOIN memories m ON m.project_id = p.id
WHERE ($1 = '' OR p.owner_id = $1)
GROUP BY p.id
ORDER BY COALESCE(MAX(m.ts), 0) DESC
LIMIT $2`

	rows, err := s.pool.Query(ctx, query, ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ProjectListItem
	for rows.Next() {
		var item ProjectListItem
		var machineName sql.NullString
		var projectPath sql.NullString
		if err := rows.Scan(&item.OwnerID, &item.ProjectKey, &machineName, &projectPath, &item.ProjectName, &item.MemoryCount, &item.LatestTs); err != nil {
			return nil, err
		}
		item.MachineName = machineName.String
		item.ProjectPath = projectPath.String
		results = append(results, item)
	}
	return results, rows.Err()
}

// MemoryVectorRow represents a memory with its vector distance for conflict detection
type MemoryVectorRow struct {
	ID          string
	ContentType string
	Distance    float64
}

// MemorySummaryRow 用于仲裁时获取旧摘要
type MemorySummaryRow struct {
	ID      string
	Summary string
}

// FetchMemorySummary 获取指定 memory 的摘要（用于仲裁）
func (s *Store) FetchMemorySummary(ctx context.Context, memoryID string) (MemorySummaryRow, error) {
	query := `SELECT id, COALESCE(summary, '') FROM memories WHERE id = $1`
	var row MemorySummaryRow
	if err := s.pool.QueryRow(ctx, query, memoryID).Scan(&row.ID, &row.Summary); err != nil {
		return MemorySummaryRow{}, err
	}
	return row, nil
}

// SearchMemoryVectors searches memories by avg_embedding for semantic conflict detection
// 只按 project_id 过滤，不按 content_type（因为类型不严格互斥）
func (s *Store) SearchMemoryVectors(ctx context.Context, vector pgvector.Vector, projectID string, limit int) ([]MemoryVectorRow, error) {
	query := `
SELECT id, content_type, (avg_embedding <=> $1) AS distance
FROM memories
WHERE project_id = $2 AND avg_embedding IS NOT NULL
ORDER BY avg_embedding <=> $1
LIMIT $3`
	rows, err := s.pool.Query(ctx, query, vector, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryVectorRow
	for rows.Next() {
		var row MemoryVectorRow
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Distance); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) SearchVectorFragments(ctx context.Context, vector pgvector.Vector, projectID, scope string, axes MemoryAxes, indexPath []string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, p.project_key, m.ts, m.chunk_count,
       COALESCE(m.axes, '{}'::jsonb), COALESCE(m.index_path, '[]'::jsonb),
       (f.embedding <=> $1) AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE m.project_id = $2`
	args := []any{vector, projectID}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query, args = appendAxesFilter(query, args, axes)
	query, args = appendIndexPathFilter(query, args, indexPath)
	query += " ORDER BY f.embedding <=> $1 LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchVectorFragmentsByOwner(ctx context.Context, vector pgvector.Vector, ownerID, scope string, axes MemoryAxes, indexPath []string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, p.project_key, m.ts, m.chunk_count,
       COALESCE(m.axes, '{}'::jsonb), COALESCE(m.index_path, '[]'::jsonb),
       (f.embedding <=> $1) AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $2`
	args := []any{vector, ownerID}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query, args = appendAxesFilter(query, args, axes)
	query, args = appendIndexPathFilter(query, args, indexPath)
	query += " ORDER BY f.embedding <=> $1 LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchKeywordFragments(ctx context.Context, keyword, projectID, scope string, axes MemoryAxes, indexPath []string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, p.project_key, m.ts, m.chunk_count,
       COALESCE(m.axes, '{}'::jsonb), COALESCE(m.index_path, '[]'::jsonb),
       0 AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE m.project_id = $1 AND f.content ILIKE $2`
	args := []any{projectID, fmt.Sprintf("%%%s%%", keyword)}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query, args = appendAxesFilter(query, args, axes)
	query, args = appendIndexPathFilter(query, args, indexPath)
	query += " ORDER BY m.ts DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchKeywordFragmentsByOwner(ctx context.Context, keyword, ownerID, scope string, axes MemoryAxes, indexPath []string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, p.project_key, m.ts, m.chunk_count,
       COALESCE(m.axes, '{}'::jsonb), COALESCE(m.index_path, '[]'::jsonb),
       0 AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $1 AND f.content ILIKE $2`
	args := []any{ownerID, fmt.Sprintf("%%%s%%", keyword)}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query, args = appendAxesFilter(query, args, axes)
	query, args = appendIndexPathFilter(query, args, indexPath)
	query += " ORDER BY m.ts DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchBM25Fragments(ctx context.Context, keyword, projectID, scope string, axes MemoryAxes, indexPath []string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, p.project_key, m.ts, m.chunk_count,
       COALESCE(m.axes, '{}'::jsonb), COALESCE(m.index_path, '[]'::jsonb),
       ts_rank_cd(to_tsvector('simple', f.content), plainto_tsquery('simple', $2)) AS rank
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE m.project_id = $1 AND to_tsvector('simple', f.content) @@ plainto_tsquery('simple', $2)`
	args := []any{projectID, keyword}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query, args = appendAxesFilter(query, args, axes)
	query, args = appendIndexPathFilter(query, args, indexPath)
	query += " ORDER BY rank DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FragmentRow
	for rows.Next() {
		var row FragmentRow
		var axesJSON []byte
		var pathJSON []byte
		if err := rows.Scan(&row.FragmentID, &row.MemoryID, &row.ChunkIndex, &row.Content, &row.ContentType, &row.ProjectKey, &row.Ts, &row.ChunkCount, &axesJSON, &pathJSON, &row.RankScore); err != nil {
			return nil, err
		}
		row.Axes = decodeAxes(axesJSON)
		row.IndexPath = decodeIndexPath(pathJSON)
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) SearchBM25FragmentsByOwner(ctx context.Context, keyword, ownerID, scope string, axes MemoryAxes, indexPath []string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, p.project_key, m.ts, m.chunk_count,
       COALESCE(m.axes, '{}'::jsonb), COALESCE(m.index_path, '[]'::jsonb),
       ts_rank_cd(to_tsvector('simple', f.content), plainto_tsquery('simple', $2)) AS rank
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $1 AND to_tsvector('simple', f.content) @@ plainto_tsquery('simple', $2)`
	args := []any{ownerID, keyword}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query, args = appendAxesFilter(query, args, axes)
	query, args = appendIndexPathFilter(query, args, indexPath)
	query += " ORDER BY rank DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FragmentRow
	for rows.Next() {
		var row FragmentRow
		var axesJSON []byte
		var pathJSON []byte
		if err := rows.Scan(&row.FragmentID, &row.MemoryID, &row.ChunkIndex, &row.Content, &row.ContentType, &row.ProjectKey, &row.Ts, &row.ChunkCount, &axesJSON, &pathJSON, &row.RankScore); err != nil {
			return nil, err
		}
		row.Axes = decodeAxes(axesJSON)
		row.IndexPath = decodeIndexPath(pathJSON)
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) FetchTagCounts(ctx context.Context, projectID, ownerID string, limit int, indexPath []string) ([]AxisCount, error) {
	query := `
SELECT value, COUNT(*) FROM (
  SELECT jsonb_array_elements_text(COALESCE(m.tags, '[]'::jsonb)) AS value
  FROM memories m
  JOIN projects p ON m.project_id = p.id
  WHERE %s
) t
WHERE value <> ''
GROUP BY value
ORDER BY COUNT(*) DESC
LIMIT $1`
	where := "p.owner_id = $2"
	args := []any{limit, ownerID}
	if strings.TrimSpace(projectID) != "" {
		where = "m.project_id = $2"
		args[1] = projectID
	}
	where, args = appendIndexPathWhere(where, args, indexPath)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(query, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []AxisCount
	for rows.Next() {
		var item AxisCount
		if err := rows.Scan(&item.Value, &item.Count); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (s *Store) FetchAxisCounts(ctx context.Context, projectID, ownerID, axis string, limit int, indexPath []string) ([]AxisCount, error) {
	if !isAxisAllowed(axis) {
		return nil, fmt.Errorf("axis 不支持")
	}
	query := `
SELECT value, COUNT(*) FROM (
  SELECT jsonb_array_elements_text(COALESCE(m.axes->'%s', '[]'::jsonb)) AS value
  FROM memories m
  JOIN projects p ON m.project_id = p.id
  WHERE %s
) t
WHERE value <> ''
GROUP BY value
ORDER BY COUNT(*) DESC
LIMIT $1`
	where := "p.owner_id = $2"
	args := []any{limit, ownerID}
	if strings.TrimSpace(projectID) != "" {
		where = "m.project_id = $2"
		args[1] = projectID
	}
	where, args = appendIndexPathWhere(where, args, indexPath)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(query, axis, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []AxisCount
	for rows.Next() {
		var item AxisCount
		if err := rows.Scan(&item.Value, &item.Count); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (s *Store) FetchIndexPaths(ctx context.Context, projectID, ownerID string, limit int, indexPath []string) ([]IndexPathCount, error) {
	query := `
SELECT m.index_path, COUNT(*) 
FROM memories m
JOIN projects p ON m.project_id = p.id
WHERE %s AND m.index_path IS NOT NULL
GROUP BY m.index_path
ORDER BY COUNT(*) DESC
LIMIT $1`
	where := "p.owner_id = $2"
	args := []any{limit, ownerID}
	if strings.TrimSpace(projectID) != "" {
		where = "m.project_id = $2"
		args[1] = projectID
	}
	where, args = appendIndexPathWhere(where, args, indexPath)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(query, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []IndexPathCount
	for rows.Next() {
		var raw []byte
		var count int
		if err := rows.Scan(&raw, &count); err != nil {
			return nil, err
		}
		results = append(results, IndexPathCount{
			Path:  decodeIndexPath(raw),
			Count: count,
		})
	}
	return results, rows.Err()
}

func (s *Store) FetchMemoryCounts(ctx context.Context, projectID, ownerID string, indexPath []string) (MemoryCounts, error) {
	query := `
SELECT COUNT(*) AS total,
       COALESCE(SUM(CASE WHEN m.axes IS NOT NULL AND m.axes != '{}'::jsonb THEN 1 ELSE 0 END), 0) AS axes_count,
       COALESCE(SUM(CASE WHEN m.index_path IS NOT NULL AND m.index_path != '[]'::jsonb THEN 1 ELSE 0 END), 0) AS path_count
FROM memories m
JOIN projects p ON m.project_id = p.id
WHERE %s`
	where := "p.owner_id = $1"
	args := []any{ownerID}
	if strings.TrimSpace(projectID) != "" {
		where = "m.project_id = $1"
		args[0] = projectID
	}
	where, args = appendIndexPathWhere(where, args, indexPath)
	row := s.pool.QueryRow(ctx, fmt.Sprintf(query, where), args...)
	var counts MemoryCounts
	if err := row.Scan(&counts.Total, &counts.Axes, &counts.IndexPath); err != nil {
		return MemoryCounts{}, err
	}
	return counts, nil
}

func (s *Store) FetchIndexPathDepthDistribution(ctx context.Context, projectID, ownerID string, indexPath []string) ([]DepthCount, error) {
	query := `
SELECT jsonb_array_length(m.index_path) AS depth, COUNT(*)
FROM memories m
JOIN projects p ON m.project_id = p.id
WHERE %s AND m.index_path IS NOT NULL AND m.index_path != '[]'::jsonb
GROUP BY depth
ORDER BY depth`
	where := "p.owner_id = $1"
	args := []any{ownerID}
	if strings.TrimSpace(projectID) != "" {
		where = "m.project_id = $1"
		args[0] = projectID
	}
	where, args = appendIndexPathWhere(where, args, indexPath)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(query, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []DepthCount
	for rows.Next() {
		var item DepthCount
		if err := rows.Scan(&item.Depth, &item.Count); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func isAxisAllowed(axis string) bool {
	switch axis {
	case "domain", "stack", "problem", "lifecycle", "component":
		return true
	default:
		return false
	}
}

func scanFragmentRows(rows pgx.Rows) ([]FragmentRow, error) {
	var results []FragmentRow
	for rows.Next() {
		var row FragmentRow
		var axesJSON []byte
		var pathJSON []byte
		if err := rows.Scan(&row.FragmentID, &row.MemoryID, &row.ChunkIndex, &row.Content, &row.ContentType, &row.ProjectKey, &row.Ts, &row.ChunkCount, &axesJSON, &pathJSON, &row.Distance); err != nil {
			return nil, err
		}
		row.Axes = decodeAxes(axesJSON)
		row.IndexPath = decodeIndexPath(pathJSON)
		results = append(results, row)
	}
	return results, rows.Err()
}

func appendAxesFilter(query string, args []any, axes MemoryAxes) (string, []any) {
	query, args = appendAxisFilter(query, args, "domain", axes.Domain)
	query, args = appendAxisFilter(query, args, "stack", axes.Stack)
	query, args = appendAxisFilter(query, args, "problem", axes.Problem)
	query, args = appendAxisFilter(query, args, "lifecycle", axes.Lifecycle)
	query, args = appendAxisFilter(query, args, "component", axes.Component)
	return query, args
}

func appendAxisFilter(query string, args []any, field string, values []string) (string, []any) {
	if len(values) == 0 {
		return query, args
	}
	query += " AND COALESCE(m.axes->'" + field + "', '[]'::jsonb) ?| $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, values)
	return query, args
}

func appendIndexPathFilter(query string, args []any, indexPath []string) (string, []any) {
	if len(indexPath) == 0 {
		return query, args
	}
	for idx, segment := range indexPath {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		query += " AND m.index_path->>" + fmt.Sprintf("%d", idx) + " = $" + fmt.Sprintf("%d", len(args)+1)
		args = append(args, segment)
	}
	return query, args
}

func appendIndexPathWhere(where string, args []any, indexPath []string) (string, []any) {
	if len(indexPath) == 0 {
		return where, args
	}
	for idx, segment := range indexPath {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		where += " AND m.index_path->>" + fmt.Sprintf("%d", idx) + " = $" + fmt.Sprintf("%d", len(args)+1)
		args = append(args, segment)
	}
	return where, args
}

func decodeTags(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal(raw, &tags); err == nil {
		return normalizeTags(tags)
	}
	return []string{}
}

func baseName(path string) string {
	trimmed := strings.TrimRight(path, "/\\")
	if trimmed == "" {
		return path
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return trimmed
	}
	return parts[len(parts)-1]
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// === 仲裁历史与回滚 ===

// FetchArbitrationHistory 查询仲裁历史
func (s *Store) FetchArbitrationHistory(ctx context.Context, ownerID, memoryID, projectID string, limit int) ([]ArbitrationRecord, error) {
	query := `
SELECT id, COALESCE(candidate_memory_id, ''), COALESCE(new_memory_id, ''), action,
       COALESCE(similarity, 0), COALESCE(old_summary, ''), COALESCE(new_summary, ''),
       COALESCE(model, ''), EXTRACT(EPOCH FROM created_at)::BIGINT
FROM memory_arbitrations
WHERE owner_id = $1`
	args := []any{ownerID}

	if memoryID != "" {
		query += " AND (candidate_memory_id = $2 OR new_memory_id = $2)"
		args = append(args, memoryID)
	}
	if projectID != "" {
		query += " AND project_id = $" + fmt.Sprintf("%d", len(args)+1)
		args = append(args, projectID)
	}
	query += " ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ArbitrationRecord
	for rows.Next() {
		var r ArbitrationRecord
		if err := rows.Scan(&r.ID, &r.CandidateMemoryID, &r.NewMemoryID, &r.Action,
			&r.Similarity, &r.OldSummary, &r.NewSummary, &r.Model, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// FetchMemoryVersions 查询记忆的历史版本
func (s *Store) FetchMemoryVersions(ctx context.Context, memoryID string) ([]MemoryVersion, error) {
	query := `
SELECT id, COALESCE(summary, ''), content_type, ts,
       EXTRACT(EPOCH FROM replaced_at)::BIGINT
FROM memory_versions
WHERE memory_id = $1
ORDER BY replaced_at DESC`

	rows, err := s.pool.Query(ctx, query, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryVersion
	for rows.Next() {
		var v MemoryVersion
		if err := rows.Scan(&v.VersionID, &v.Summary, &v.ContentType, &v.Ts, &v.ReplacedAt); err != nil {
			return nil, err
		}
		results = append(results, v)
	}
	return results, rows.Err()
}

// FetchArbitrationByID 根据 ID 获取仲裁记录
func (s *Store) FetchArbitrationByID(ctx context.Context, id int64) (ArbitrationRecord, error) {
	query := `
SELECT id, COALESCE(candidate_memory_id, ''), COALESCE(new_memory_id, ''), action,
       COALESCE(similarity, 0), COALESCE(old_summary, ''), COALESCE(new_summary, ''),
       COALESCE(model, ''), EXTRACT(EPOCH FROM created_at)::BIGINT
FROM memory_arbitrations
WHERE id = $1`

	var r ArbitrationRecord
	err := s.pool.QueryRow(ctx, query, id).Scan(&r.ID, &r.CandidateMemoryID, &r.NewMemoryID, &r.Action,
		&r.Similarity, &r.OldSummary, &r.NewSummary, &r.Model, &r.CreatedAt)
	return r, err
}

// FetchLatestVersion 获取记忆的最新历史版本
func (s *Store) FetchLatestVersion(ctx context.Context, memoryID string) (MemoryVersionInsert, error) {
	query := `
SELECT memory_id, project_id, content_type, content, COALESCE(content_hash, ''), ts,
       COALESCE(summary, ''), COALESCE(tags, '[]'::jsonb), COALESCE(axes, '{}'::jsonb),
       COALESCE(index_path, '[]'::jsonb), COALESCE(chunk_count, 1), avg_embedding,
       created_at, replaced_at
FROM memory_versions
WHERE memory_id = $1
ORDER BY replaced_at DESC
LIMIT 1`

	var v MemoryVersionInsert
	var tagsJSON, axesJSON, indexPathJSON []byte
	var avgEmbedding pgvector.Vector
	err := s.pool.QueryRow(ctx, query, memoryID).Scan(
		&v.MemoryID, &v.ProjectID, &v.ContentType, &v.Content, &v.ContentHash, &v.Ts,
		&v.Summary, &tagsJSON, &axesJSON, &indexPathJSON, &v.ChunkCount, &avgEmbedding,
		&v.CreatedAt, &v.ReplacedAt,
	)
	if err != nil {
		return v, err
	}
	v.Tags = decodeTags(tagsJSON)
	v.Axes = decodeAxes(axesJSON)
	v.IndexPath = decodeIndexPath(indexPathJSON)
	v.AvgEmbedding = avgEmbedding.Slice()
	return v, nil
}

// RestoreMemoryFromVersion 从历史版本恢复记忆
func (s *Store) RestoreMemoryFromVersion(ctx context.Context, version MemoryVersionInsert) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 先把当前版本保存到 memory_versions
	_, err = tx.Exec(ctx, `
INSERT INTO memory_versions (memory_id, project_id, content_type, content, content_hash, ts, summary, tags, axes, index_path, chunk_count, avg_embedding, created_at, replaced_at)
SELECT id, project_id, content_type, content, content_hash, ts, summary, tags, axes, index_path, chunk_count, avg_embedding, created_at, NOW()
FROM memories WHERE id = $1`, version.MemoryID)
	if err != nil {
		return fmt.Errorf("保存当前版本失败: %w", err)
	}

	// 更新 memories 表
	tagsJSON, _ := json.Marshal(version.Tags)
	axesJSON, _ := json.Marshal(version.Axes)
	indexPathJSON, _ := json.Marshal(version.IndexPath)

	_, err = tx.Exec(ctx, `
UPDATE memories SET
  content_type = $2, content = $3, content_hash = $4, ts = $5,
  summary = $6, tags = $7, axes = $8, index_path = $9,
  chunk_count = $10, avg_embedding = $11, created_at = $12
WHERE id = $1`,
		version.MemoryID, version.ContentType, version.Content, version.ContentHash, version.Ts,
		version.Summary, tagsJSON, axesJSON, indexPathJSON,
		version.ChunkCount, pgvector.NewVector(version.AvgEmbedding), version.CreatedAt)
	if err != nil {
		return fmt.Errorf("恢复记忆失败: %w", err)
	}

	// 删除用于恢复的那个历史版本记录（避免重复）
	_, err = tx.Exec(ctx, `
DELETE FROM memory_versions
WHERE memory_id = $1 AND replaced_at = $2`, version.MemoryID, version.ReplacedAt)
	if err != nil {
		return fmt.Errorf("清理历史版本失败: %w", err)
	}

	return tx.Commit(ctx)
}
