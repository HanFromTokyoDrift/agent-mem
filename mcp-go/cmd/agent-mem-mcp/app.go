package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type App struct {
	settings Settings
	store    *Store
	llm      *LLMClient
	embedder *Embedder
	searcher *Searcher
}

func NewApp(settings Settings) (*App, error) {
	store, err := NewStore(settings.Storage.DatabaseURL)
	if err != nil {
		return nil, err
	}
	llm := NewLLMClient(settings)
	embedder := NewEmbedder(settings)
	searcher := NewSearcher(store, llm, embedder, settings)

	return &App{
		settings: settings,
		store:    store,
		llm:      llm,
		embedder: embedder,
		searcher: searcher,
	}, nil
}

func (a *App) Close() {
	if a.store != nil {
		a.store.Close()
	}
}

func (a *App) EnsureSchema(ctx context.Context) error {
	return a.store.EnsureSchema(ctx, a.settings.Embedding.Dimension)
}

func buildServer(app *App) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "agent-mem", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: "这是 Agent Memory 的 MCP 服务。建议流程：mem.search -> mem.timeline -> mem.get_observations，写入使用 mem.write_memory。",
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.write_memory",
		Description: "写入结构化记忆并触发入库",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in WriteMemoryInput) (*mcp.CallToolResult, WriteMemoryOutput, error) {
		output, err := app.WriteMemory(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.search",
		Description: "语义检索知识索引（默认使用意图路由）",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, []map[string]any, error) {
		results, err := app.Search(ctx, in)
		return nil, results, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.get_observations",
		Description: "批量获取完整记忆详情",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, ids []string) (*mcp.CallToolResult, []map[string]any, error) {
		results, err := app.GetObservations(ctx, ids)
		return nil, results, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.timeline",
		Description: "按时间窗口获取上下文列表",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TimelineInput) (*mcp.CallToolResult, []map[string]any, error) {
		results, err := app.Timeline(ctx, in)
		return nil, results, err
	})

	return server
}

func (a *App) WriteMemory(ctx context.Context, in WriteMemoryInput) (WriteMemoryOutput, error) {
	if strings.TrimSpace(in.ProjectRoot) == "" {
		return WriteMemoryOutput{}, errors.New("project_root 必填")
	}
	if strings.TrimSpace(in.Content) == "" {
		return WriteMemoryOutput{}, errors.New("content 不能为空")
	}

	relativePath := strings.TrimSpace(in.RelativePath)
	if relativePath == "" {
		relativePath = autoRelativePath(in.Content, in.KnowledgeType, in.InsightType)
	}

	root, target, rel, err := safeResolvePath(in.ProjectRoot, relativePath)
	if err != nil {
		return WriteMemoryOutput{}, err
	}

	if exists(target) && !in.Overwrite {
		target = appendSuffix(target)
		rel, _ = filepath.Rel(root, target)
	}

	if err := ensureDir(filepath.Dir(target)); err != nil {
		return WriteMemoryOutput{}, err
	}

	tags := normalizeTags(in.Tags)
	finalContent := ensureFrontMatter(in.Content, in.KnowledgeType, in.InsightType, tags)
	if err := writeText(target, finalContent); err != nil {
		return WriteMemoryOutput{}, err
	}

	projectMeta := loadProjectMeta(a.settings, root)
	result := WriteMemoryOutput{
		Status:       "ok",
		FilePath:     target,
		RelativePath: filepath.ToSlash(rel),
		ProjectID:    projectMeta.ProjectID,
	}

	ingestResult, err := ingestFile(ctx, a, target, root, envOrDefault("HOST_ID", "mcp-go"))
	if err != nil {
		result.IngestStatus = "error"
		result.Reason = err.Error()
		return result, nil
	}
	result.IngestStatus = ingestResult.Status
	result.Reason = ingestResult.Reason
	return result, nil
}

func (a *App) Search(ctx context.Context, in SearchInput) ([]map[string]any, error) {
	return a.searcher.Search(ctx, in)
}

func (a *App) GetObservations(ctx context.Context, ids []string) ([]map[string]any, error) {
	if len(ids) == 0 {
		return []map[string]any{}, nil
	}

	rows, err := a.store.FetchObservations(ctx, ids)
	if err != nil {
		return nil, err
	}
	resultMap := make(map[string]map[string]any)
	for _, row := range rows {
		resultMap[row["id"].(string)] = row
	}
	ordered := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if row, ok := resultMap[id]; ok {
			ordered = append(ordered, row)
		}
	}
	return ordered, nil
}

func (a *App) Timeline(ctx context.Context, in TimelineInput) ([]map[string]any, error) {
	anchorID := strings.TrimSpace(in.AnchorID)
	if anchorID == "" && strings.TrimSpace(in.Query) != "" {
		results, err := a.searcher.Search(ctx, SearchInput{Query: in.Query, Limit: intPtr(1), UseRouting: boolPtr(true)})
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			if id, ok := results[0]["id"].(string); ok {
				anchorID = id
			}
		}
	}

	if anchorID == "" {
		return []map[string]any{}, nil
	}

	anchor, err := a.store.FetchAnchor(ctx, anchorID)
	if err != nil {
		return nil, err
	}
	if anchor == nil {
		return []map[string]any{}, nil
	}

	anchorTime := anchor.UpdatedAt
	if anchorTime.IsZero() {
		anchorTime = anchor.CreatedAt
	}
	if anchorTime.IsZero() {
		return []map[string]any{}, nil
	}

	days := 3
	if in.Days != nil && *in.Days > 0 {
		days = *in.Days
	}
	limit := 10
	if in.Limit != nil && *in.Limit > 0 {
		limit = *in.Limit
	}

	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		projectID = anchor.ProjectID
	}

	start := anchorTime.Add(-time.Duration(days) * 24 * time.Hour)
	end := anchorTime.Add(time.Duration(days) * 24 * time.Hour)

	return a.store.FetchTimeline(ctx, projectID, start, end, limit)
}
