package main

import (
	"time"

	"github.com/pgvector/pgvector-go"
)

type WriteMemoryInput struct {
	ProjectRoot   string   `json:"project_root" jsonschema:"description=项目根目录"`
	RelativePath  string   `json:"relative_path,omitempty" jsonschema:"description=相对路径，可选"`
	Content       string   `json:"content" jsonschema:"description=Markdown 内容"`
	KnowledgeType string   `json:"knowledge_type,omitempty" jsonschema:"description=doc/insight/dialogue_extract"`
	InsightType   string   `json:"insight_type,omitempty" jsonschema:"description=solution/lesson/pattern/decision"`
	Tags          []string `json:"tags,omitempty" jsonschema:"description=标签"`
	Overwrite     bool     `json:"overwrite,omitempty" jsonschema:"description=覆盖已有文件"`
}

type WriteMemoryOutput struct {
	Status       string `json:"status"`
	FilePath     string `json:"file_path,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	IngestStatus string `json:"ingest_status,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type SearchInput struct {
	Query          string   `json:"query" jsonschema:"description=检索问题"`
	ProjectID      string   `json:"project_id,omitempty"`
	DocTypes       []string `json:"doc_types,omitempty"`
	KnowledgeTypes []string `json:"knowledge_types,omitempty"`
	Limit          *int     `json:"limit,omitempty"`
	UseRouting     *bool    `json:"use_routing,omitempty"`
	UseRerank      *bool    `json:"use_rerank,omitempty"`
}

type TimelineInput struct {
	ProjectID string `json:"project_id,omitempty"`
	AnchorID  string `json:"anchor_id,omitempty"`
	Query     string `json:"query,omitempty"`
	Days      *int   `json:"days,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

type KnowledgeBlock struct {
	ID                string           `json:"id"`
	KnowledgeType     string           `json:"knowledge_type"`
	DocType           string           `json:"doc_type,omitempty"`
	InsightType       string           `json:"insight_type,omitempty"`
	SourceType        string           `json:"source_type,omitempty"`
	RawContentPath    string           `json:"raw_content_path,omitempty"`
	ProjectID         string           `json:"project_id"`
	ProjectName       string           `json:"project_name,omitempty"`
	MachineID         string           `json:"machine_id,omitempty"`
	FilePath          string           `json:"file_path"`
	RelativePath      string           `json:"relative_path"`
	FileHash          string           `json:"file_hash"`
	Title             string           `json:"title"`
	Content           string           `json:"content"`
	Summary           string           `json:"summary,omitempty"`
	StructuredContent any              `json:"structured_content,omitempty"`
	CategoryL1        string           `json:"category_l1,omitempty"`
	CategoryL2        string           `json:"category_l2,omitempty"`
	CategoryL3        string           `json:"category_l3,omitempty"`
	Tags              []string         `json:"tags,omitempty"`
	Embedding         pgvector.Vector  `json:"embedding"`
	RelatedIDs        any              `json:"related_ids,omitempty"`
	Version           int              `json:"version"`
	IsLatest          bool             `json:"is_latest"`
	SupersededBy      string           `json:"superseded_by,omitempty"`
	SupersedeReason   string           `json:"supersede_reason,omitempty"`
	Status            string           `json:"status"`
	DecayRule         string           `json:"decay_rule,omitempty"`
	ExpiresAt         *time.Time       `json:"expires_at,omitempty"`
	IsHighValue       bool             `json:"is_high_value"`
	Reproducible      bool             `json:"reproducible"`
	ApplicableTo      []string         `json:"applicable_to,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}