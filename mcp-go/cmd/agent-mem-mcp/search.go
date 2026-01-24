package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
)

type Searcher struct {
	store    *Store
	llm      *LLMClient
	embedder *Embedder
	settings Settings
}

type Route struct {
	DocTypes       []string
	MustLatest     bool
	TimeFilterDays *int
	OrderBy        string
}

type SearchRow struct {
	ID            string
	Title         string
	FilePath      string
	Summary       string
	Content       string
	DocType       string
	KnowledgeType string
	ProjectID     string
	Score         float64
}

func NewSearcher(store *Store, llm *LLMClient, embedder *Embedder, settings Settings) *Searcher {
	return &Searcher{store: store, llm: llm, embedder: embedder, settings: settings}
}

func (s *Searcher) Search(ctx context.Context, in SearchInput) ([]map[string]any, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}

	limit := 5
	if in.Limit != nil && *in.Limit > 0 {
		limit = *in.Limit
	}
	useRouting := true
	if in.UseRouting != nil {
		useRouting = *in.UseRouting
	}
	useRerank := s.settings.Rerank.Enabled
	if in.UseRerank != nil {
		useRerank = *in.UseRerank
	}

	docTypes := append([]string{}, in.DocTypes...)
	knowledgeTypes := append([]string{}, in.KnowledgeTypes...)
	mustLatest := true
	var timeFilterDays *int
	orderBy := "relevance"

	if useRouting {
		route := s.llm.RouteQuery(query)
		mustLatest = route.MustLatest
		timeFilterDays = route.TimeFilterDays
		if route.OrderBy != "" {
			orderBy = route.OrderBy
		}
		for _, value := range route.DocTypes {
			if value == "insight" || value == "dialogue_extract" {
				knowledgeTypes = append(knowledgeTypes, value)
			} else {
				docTypes = append(docTypes, value)
			}
		}
	}

	vector, err := s.embedder.EmbedQuery(query)
	if err != nil {
		return nil, err
	}

	if orderBy == "time_desc" {
		useRerank = false
	}

	initialLimit := limit
	if useRerank {
		initialLimit = limit * 5
	}

	params := SearchParams{
		ProjectID:      strings.TrimSpace(in.ProjectID),
		DocTypes:       uniqueStrings(docTypes),
		KnowledgeTypes: uniqueStrings(knowledgeTypes),
		Limit:          initialLimit,
		MustLatest:     mustLatest,
		OrderBy:        orderBy,
	}
	if timeFilterDays != nil {
		since := time.Now().UTC().Add(-time.Duration(*timeFilterDays) * 24 * time.Hour)
		params.Since = &since
	}

	rows, err := s.store.SearchVector(ctx, vector, params)
	if err != nil {
		return nil, err
	}

	if !useRerank || len(rows) == 0 {
		results := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			results = append(results, map[string]any{
				"id":             row.ID,
				"title":          row.Title,
				"file_path":      row.FilePath,
				"summary":        row.Summary,
				"doc_type":       row.DocType,
				"knowledge_type": row.KnowledgeType,
				"score":          row.Score,
				"project_id":     row.ProjectID,
			})
		}
		if len(results) > limit {
			return results[:limit], nil
		}
		return results, nil
	}

	docs := make([]string, 0, len(rows))
	for _, row := range rows {
		text := strings.TrimSpace(row.Summary) + "\n" + strings.TrimSpace(row.Content)
		docs = append(docs, truncate(text, 2000))
	}

	rerank, err := s.llm.Rerank(query, docs, limit)
	if err != nil || len(rerank) == 0 {
		results := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			results = append(results, map[string]any{
				"id":             row.ID,
				"title":          row.Title,
				"file_path":      row.FilePath,
				"summary":        row.Summary,
				"doc_type":       row.DocType,
				"knowledge_type": row.KnowledgeType,
				"score":          row.Score,
				"project_id":     row.ProjectID,
			})
		}
		if len(results) > limit {
			return results[:limit], nil
		}
		return results, nil
	}

	results := make([]map[string]any, 0, len(rerank))
	for _, item := range rerank {
		if item.Index < 0 || item.Index >= len(rows) {
			continue
		}
		row := rows[item.Index]
		results = append(results, map[string]any{
			"id":             row.ID,
			"title":          row.Title,
			"file_path":      row.FilePath,
			"summary":        row.Summary,
			"doc_type":       row.DocType,
			"knowledge_type": row.KnowledgeType,
			"score":          item.RelevanceScore,
			"project_id":     row.ProjectID,
			"is_reranked":    true,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		scoreI, _ := results[i]["score"].(float64)
		scoreJ, _ := results[j]["score"].(float64)
		return scoreI > scoreJ
	})
	if len(results) > limit {
		return results[:limit], nil
	}
	return results, nil
}

func (s *Searcher) SearchSimilar(ctx context.Context, vector pgvector.Vector, projectID string, docType string, limit int) ([]map[string]any, error) {
	return s.store.SearchSimilar(ctx, vector, projectID, docType, limit)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
