package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
	"gopkg.in/yaml.v3"
)

type KnowledgeType string

type DocType string

type InsightType string

type SourceType string

type StatusType string

type DecayRule string

const (
	KnowledgeTypeDoc             KnowledgeType = "doc"
	KnowledgeTypeInsight         KnowledgeType = "insight"
	KnowledgeTypeDialogueExtract KnowledgeType = "dialogue_extract"

	SourceTypeFile     SourceType = "file"
	SourceTypeDialogue SourceType = "dialogue"

	StatusActive     StatusType = "active"
	StatusDeprecated StatusType = "deprecated"
	StatusConflict   StatusType = "conflict"

	DecayNone        DecayRule = "none"
	DecayTime30Days  DecayRule = "time_30d"
	DecayVersionOnly DecayRule = "version_only"
)

type KnowledgeIngest struct {
	ProjectID      string
	ProjectName    string
	MachineID      string
	FilePath       string
	RelativePath   string
	RawContentPath string
	FileHash       string
	Title          string
	Content        string
	Summary        string
	Structured     map[string]any
	KnowledgeType  KnowledgeType
	DocType        DocType
	InsightType    InsightType
	SourceType     SourceType
	CategoryL1     string
	CategoryL2     string
	CategoryL3     string
	Tags           []string
	RelatedIDs     []map[string]any
	DecayRule      DecayRule
	IsHighValue    bool
	Reproducible   *bool
	ApplicableTo   []string
}

type IngestResult struct {
	Status string
	Reason string
	ID     string
}

type ProjectMeta struct {
	ProjectID   string
	ProjectName string
	RootPath    string
}

var docTypeRules = []struct {
	Pattern *regexp.Regexp
	Type    DocType
}{
	{regexp.MustCompile(`docs/background/`), DocType("background")},
	{regexp.MustCompile(`docs/requirements?/`), DocType("requirements")},
	{regexp.MustCompile(`docs/arch(itecture)?/`), DocType("architecture")},
	{regexp.MustCompile(`docs/design/`), DocType("design")},
	{regexp.MustCompile(`docs/implementation/`), DocType("implementation")},
	{regexp.MustCompile(`docs/progress/`), DocType("progress")},
	{regexp.MustCompile(`docs/testing/`), DocType("testing")},
	{regexp.MustCompile(`docs/deploy(ment)?/`), DocType("deployment")},
	{regexp.MustCompile(`docs/delivery/`), DocType("delivery")},
}

var rootFileRules = map[string]DocType{
	"readme.md":       DocType("delivery"),
	"tasks.md":        DocType("progress"),
	"changelog.md":    DocType("progress"),
	"todo.md":         DocType("progress"),
	"notes.md":        DocType("progress"),
	"design.md":       DocType("architecture"),
	"architecture.md": DocType("architecture"),
}

var insightPathRules = []struct {
	Pattern *regexp.Regexp
	Type    InsightType
}{
	{regexp.MustCompile(`insights?/`), InsightType("pattern")},
	{regexp.MustCompile(`lessons?/`), InsightType("lesson")},
	{regexp.MustCompile(`postmortem/`), InsightType("lesson")},
}

var dialogueRules = []*regexp.Regexp{
	regexp.MustCompile(`chat_history/`),
	regexp.MustCompile(`\.claude/`),
	regexp.MustCompile(`\.codex/`),
	regexp.MustCompile(`\.gemini/`),
}

var decayRules = map[string]DecayRule{
	"progress":   DecayTime30Days,
	"deployment": DecayVersionOnly,
	"delivery":   DecayVersionOnly,
}

func ingestFile(ctx context.Context, app *App, filePath, projectRoot, machineID string) (IngestResult, error) {
	data, err := processFile(app.settings, filePath, projectRoot, machineID)
	if err != nil {
		return IngestResult{}, err
	}
	if data == nil {
		return IngestResult{Status: "skipped", Reason: "文件不在监控范围或为空"}, nil
	}

	existing, err := app.store.FindLatestByRelativePath(ctx, data.ProjectID, data.RelativePath)
	if err != nil {
		return IngestResult{}, err
	}
	if existing != nil && existing.FileHash == data.FileHash {
		return IngestResult{Status: "skipped", Reason: "未变化"}, nil
	}

	if data.SourceType == SourceTypeDialogue {
		distilled := app.llm.DistillDialogue(data.Content, data.ProjectID)
		data.Summary = distilled.Summary
		data.KnowledgeType = KnowledgeTypeDialogueExtract
		if isValidInsightType(distilled.InsightType) {
			data.InsightType = InsightType(distilled.InsightType)
		}
		data.Structured = map[string]any{
			"problem":  distilled.Problem,
			"thinking": distilled.Thinking,
			"solution": distilled.Solution,
			"result":   distilled.Result,
		}
		if distilled.Solution != "" {
			data.Content = distilled.Solution
		}
		data.IsHighValue = true
		data.Tags = mergeTags(data.Tags, distilled.Tags)
		data.Reproducible = &distilled.Reproducible
		data.ApplicableTo = distilled.ApplicableTo
		data.RawContentPath = data.FilePath
	}

	if data.Summary == "" && len(data.Content) > 800 {
		data.Summary = app.llm.Summarize(data.Content)
	}

	data.RelatedIDs = resolveRelations(ctx, app, data.Content, data.ProjectID)

	vector, err := app.embedder.EmbedQuery(data.SummaryOrContent())
	if err != nil {
		return IngestResult{}, err
	}

	id := newID()
	version := 1
	if existing != nil {
		version = existing.Version + 1
	}

	now := time.Now().UTC()
	expiresAt := calcExpiresAt(data.DecayRule, now)

	structuredJSON, err := json.Marshal(data.Structured)
	if err != nil {
		return IngestResult{}, err
	}
	tagsJSON, err := json.Marshal(data.Tags)
	if err != nil {
		return IngestResult{}, err
	}
	relatedJSON, err := json.Marshal(data.RelatedIDs)
	if err != nil {
		return IngestResult{}, err
	}
	applicableJSON, err := json.Marshal(data.ApplicableTo)
	if err != nil {
		return IngestResult{}, err
	}

	tx, err := app.store.Begin(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	insert := `
INSERT INTO knowledge (
  id, knowledge_type, doc_type, insight_type, source_type, raw_content_path,
  project_id, project_name, machine_id, file_path, relative_path, file_hash,
  title, content, summary, structured_content, category_l1, category_l2, category_l3,
  tags, embedding, related_ids, version, is_latest, superseded_by, supersede_reason,
  status, decay_rule, expires_at, is_high_value, reproducible, applicable_to,
  created_at, updated_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34
)`

	_, err = tx.Exec(ctx, insert,
		id,
		string(data.KnowledgeType),
		nullableString(string(data.DocType)),
		nullableString(string(data.InsightType)),
		string(data.SourceType),
		nullableString(data.RawContentPath),
		data.ProjectID,
		nullableString(data.ProjectName),
		data.MachineID,
		data.FilePath,
		data.RelativePath,
		data.FileHash,
		data.Title,
		data.Content,
		nullableString(data.Summary),
		nullableJSON(structuredJSON),
		nullableString(data.CategoryL1),
		nullableString(data.CategoryL2),
		nullableString(data.CategoryL3),
		nullableJSON(tagsJSON),
		vector,
		nullableJSON(relatedJSON),
		version,
		true,
		nil,
		nil,
		string(StatusActive),
		string(data.DecayRule),
		nullableTime(expiresAt),
		data.IsHighValue,
		nullableBool(data.Reproducible),
		nullableJSON(applicableJSON),
		now,
		now,
	)
	if err != nil {
		return IngestResult{}, err
	}

	if existing != nil {
		// 极客模式：同一文件更新，直接物理删除旧记录
		if err := app.store.DeleteBlock(ctx, tx, existing.ID); err != nil {
			return IngestResult{}, err
		}
	} else {
		// 新文件：检查语义冲突
		if err := semanticReplace(ctx, app, tx, id, data, vector); err != nil {
			return IngestResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, err
	}

	return IngestResult{Status: "ok", ID: id}, nil
}

func semanticReplace(ctx context.Context, app *App, tx pgx.Tx, newID string, data *KnowledgeIngest, vector pgvector.Vector) error {
	candidates, err := app.store.SearchSimilar(ctx, vector, data.ProjectID, string(data.DocType), 3)
	if err != nil {
		return err
	}
	threshold := app.settings.Versioning.SemanticSimilarityThreshold
	for _, candidate := range candidates {
		similarity, _ := candidate["similarity"].(float64)
		if similarity < threshold {
			continue
		}
		// 只有相似度够高，才进行 LLM 仲裁
		decision := app.llm.ArbitrateConflict(data.Content, candidate["content"].(string))
		switch decision {
		case "replace":
			// 极客模式：语义替代，直接物理删除旧记录
			if err := app.store.DeleteBlock(ctx, tx, candidate["id"].(string)); err != nil {
				return err
			}
		case "conflict":
			// 冲突时保留旧的，标记为 conflict
			if err := markSuperseded(ctx, tx, candidate["id"].(string), newID, StatusConflict, "conflict"); err != nil {
				return err
			}
		default:
			continue
		}
	}
	return nil
}

func markSuperseded(ctx context.Context, tx pgx.Tx, oldID, newID string, status StatusType, reason string) error {
	update := `UPDATE knowledge SET is_latest = false, superseded_by = $1, status = $2, supersede_reason = $3 WHERE id = $4`
	_, err := tx.Exec(ctx, update, newID, string(status), reason, oldID)
	return err
}

func resolveRelations(ctx context.Context, app *App, content, projectID string) []map[string]any {
	relations := app.llm.ExtractRelations(content)
	var related []map[string]any
	for _, rel := range relations {
		if rel.Keyword == "" || rel.RelationType == "" {
			continue
		}
		matches, err := app.store.SearchByKeyword(ctx, projectID, rel.Keyword, 1)
		if err != nil || len(matches) == 0 {
			continue
		}
		related = append(related, map[string]any{
			"id":      matches[0]["id"],
			"type":    rel.RelationType,
			"keyword": rel.Keyword,
		})
	}
	return related
}

func processFile(settings Settings, filePath, projectRoot, machineID string) (*KnowledgeIngest, error) {
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return nil, nil
	}
	if settings.Watcher.MaxFileSizeKB > 0 && info.Size() > int64(settings.Watcher.MaxFileSizeKB)*1024 {
		return nil, nil
	}

	rootPath := projectRoot
	if rootPath == "" {
		rootPath = findProjectRoot(settings, filePath)
	}
	if rootPath == "" {
		rootPath = filepath.Dir(filePath)
	}

	relative := filePath
	if rootPath != "" {
		if rel, err := filepath.Rel(rootPath, filePath); err == nil {
			relative = rel
		}
	}
	relative = filepath.ToSlash(relative)

	if !shouldWatchFile(settings, relative) && !isDialoguePath(relative) {
		return nil, nil
	}

	content, err := readFileSafe(filePath)
	if err != nil || strings.TrimSpace(content) == "" {
		return nil, nil
	}

	front, body := parseFrontMatter(content)

	projectMeta := loadProjectMeta(settings, rootPath)
	docType := inferDocType(relative, front)
	knowledgeType, insightType, sourceType := inferKnowledgeType(relative, front)

	title := extractTitle(body, filepath.Base(relative))
	fileHash := calculateFileHash(content)

	cat1, cat2, cat3 := extractCategories(relative)

	tags := []string{}
	if rawTags, ok := front["tags"].([]any); ok {
		for _, tag := range rawTags {
			if value, ok := tag.(string); ok {
				tags = append(tags, value)
			}
		}
	}

	decayRule := decayRules[string(docType)]
	if decayRule == "" {
		decayRule = DecayNone
	}

	return &KnowledgeIngest{
		ProjectID:     projectMeta.ProjectID,
		ProjectName:   projectMeta.ProjectName,
		MachineID:     machineID,
		FilePath:      filePath,
		RelativePath:  filepath.ToSlash(relative),
		FileHash:      fileHash,
		Title:         title,
		Content:       body,
		KnowledgeType: knowledgeType,
		DocType:       docType,
		InsightType:   insightType,
		SourceType:    sourceType,
		CategoryL1:    cat1,
		CategoryL2:    cat2,
		CategoryL3:    cat3,
		Tags:          tags,
		RelatedIDs:    []map[string]any{},
		DecayRule:     decayRule,
		IsHighValue:   knowledgeType == KnowledgeTypeInsight || knowledgeType == KnowledgeTypeDialogueExtract,
	}, nil
}

func inferDocType(relativePath string, front map[string]any) DocType {
	if value, ok := front["doc_type"].(string); ok {
		return DocType(value)
	}
	filename := strings.ToLower(filepath.Base(relativePath))
	if value, ok := rootFileRules[filename]; ok {
		return value
	}
	pathLower := strings.ToLower(relativePath)
	for _, rule := range docTypeRules {
		if rule.Pattern.MatchString(pathLower) {
			return rule.Type
		}
	}
	return ""
}

func inferKnowledgeType(relativePath string, front map[string]any) (KnowledgeType, InsightType, SourceType) {
	knowledgeType := KnowledgeTypeDoc
	if value, ok := front["knowledge_type"].(string); ok {
		if value != "" {
			knowledgeType = KnowledgeType(strings.TrimSpace(value))
		}
	}
	var insightType InsightType
	if value, ok := front["insight_type"].(string); ok {
		insightType = InsightType(strings.TrimSpace(value))
	}

	pathLower := strings.ToLower(relativePath)
	for _, rule := range dialogueRules {
		if rule.MatchString(pathLower) {
			return KnowledgeTypeDialogueExtract, insightType, SourceTypeDialogue
		}
	}

	for _, rule := range insightPathRules {
		if rule.Pattern.MatchString(pathLower) {
			if insightType != "" {
				return KnowledgeTypeInsight, insightType, SourceTypeFile
			}
			return KnowledgeTypeInsight, rule.Type, SourceTypeFile
		}
	}

	return knowledgeType, insightType, SourceTypeFile
}

func shouldWatchFile(settings Settings, relativePath string) bool {
	base := filepath.Base(relativePath)
	for _, name := range settings.Watcher.WatchRoot {
		if name == base {
			return true
		}
	}

	ext := filepath.Ext(relativePath)
	if ext != "" {
		allowed := false
		for _, value := range settings.Watcher.Extensions {
			if value == ext {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for _, part := range parts {
		for _, ignore := range settings.Watcher.IgnoreDirs {
			if part == ignore {
				return false
			}
		}
	}

	if len(parts) > 0 {
		top := parts[0]
		for _, dir := range settings.Watcher.WatchDirs {
			if top == dir {
				return true
			}
		}
	}
	return false
}

func isDialoguePath(relativePath string) bool {
	pathLower := strings.ToLower(relativePath)
	for _, rule := range dialogueRules {
		if rule.MatchString(pathLower) {
			return true
		}
	}
	return false
}

func loadProjectMeta(settings Settings, projectRoot string) ProjectMeta {
	meta := ProjectMeta{ProjectID: settings.Project.DefaultProjectID, RootPath: projectRoot}
	if projectRoot == "" {
		return meta
	}
	base := filepath.Base(projectRoot)
	if base != "" && base != string(filepath.Separator) {
		meta.ProjectID = base
	}
	configPath := filepath.Join(projectRoot, ".project.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return meta
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return meta
	}
	if value, ok := raw[settings.Project.ProjectIDKey].(string); ok && value != "" {
		meta.ProjectID = value
	}
	if value, ok := raw[settings.Project.ProjectNameKey].(string); ok && value != "" {
		meta.ProjectName = value
	}
	return meta
}

func findProjectRoot(settings Settings, filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		for _, marker := range settings.Project.RootMarkers {
			if exists(filepath.Join(dir, marker)) {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func extractCategories(relativePath string) (string, string, string) {
	path := filepath.ToSlash(relativePath)
	parts := []string{}
	for _, part := range strings.Split(path, "/") {
		if part == "docs" || part == "doc" || part == "specs" {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", "", ""
	}
	last := parts[len(parts)-1]
	if ext := filepath.Ext(last); ext != "" {
		parts[len(parts)-1] = strings.TrimSuffix(last, ext)
	}
	if len(parts) == 1 {
		return parts[0], "", ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1], ""
	}
	return parts[0], parts[1], parts[2]
}

func isValidInsightType(value string) bool {
	switch value {
	case "solution", "lesson", "pattern", "decision":
		return true
	default:
		return false
	}
}

func mergeTags(tags []string, extra []string) []string {
	return normalizeTags(append(tags, extra...))
}

func calcExpiresAt(rule DecayRule, now time.Time) *time.Time {
	if rule == DecayTime30Days {
		value := now.Add(30 * 24 * time.Hour)
		return &value
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func (k *KnowledgeIngest) SummaryOrContent() string {
	if strings.TrimSpace(k.Summary) != "" {
		return k.Summary
	}
	return k.Content
}
