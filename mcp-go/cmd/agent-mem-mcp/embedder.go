package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"
)

type Embedder struct {
	provider   string
	model      string
	dimension  int
	batchSize  int
	client     *QwenClient
	mu         sync.Mutex
	queryCache map[string]cachedVector
}

type cachedVector struct {
	Value   []float32
	Expires time.Time
}

func NewEmbedder(settings Settings) *Embedder {
	provider := strings.ToLower(strings.TrimSpace(settings.Embedding.Provider))
	if provider == "" {
		provider = "qwen"
	}
	return &Embedder{
		provider:   provider,
		model:      settings.Embedding.Model,
		dimension:  settings.Embedding.Dimension,
		batchSize:  settings.Embedding.BatchSize,
		client:     NewQwenClient(settings),
		queryCache: map[string]cachedVector{},
	}
}

func (e *Embedder) EmbedQuery(text string) (pgvector.Vector, error) {
	cacheKey := e.cacheKey(text)
	if cached, ok := e.getCachedVector(cacheKey); ok {
		return pgvector.NewVector(cached), nil
	}
	vectors, err := e.embed(context.Background(), []string{text})
	if err != nil {
		return pgvector.NewVector([]float32{}), err
	}
	if len(vectors) == 0 {
		return pgvector.NewVector(make([]float32, e.dimension)), nil
	}
	vector := vectors[0].Slice()
	if len(vector) > 0 {
		e.setCachedVector(cacheKey, vector)
	}
	return vectors[0], nil
}

func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	vectors, err := e.embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	result := make([][]float32, 0, len(vectors))
	for _, vector := range vectors {
		result = append(result, vector.Slice())
	}
	return result, nil
}

func (e *Embedder) embed(ctx context.Context, texts []string) ([]pgvector.Vector, error) {
	if len(texts) == 0 {
		return []pgvector.Vector{}, nil
	}

	switch e.provider {
	case "mock":
		result := make([]pgvector.Vector, 0, len(texts))
		for _, text := range texts {
			vector := e.normalize(e.mockEmbed(text))
			result = append(result, pgvector.NewVector(vector))
		}
		return result, nil
	case "qwen":
		if e.model == "" {
			return nil, fmt.Errorf("缺少向量模型配置")
		}
		batchSize := e.batchSize
		if batchSize <= 0 {
			batchSize = 10
		}
		if batchSize > 10 {
			batchSize = 10
		}
		result := make([]pgvector.Vector, 0, len(texts))
		for start := 0; start < len(texts); start += batchSize {
			end := start + batchSize
			if end > len(texts) {
				end = len(texts)
			}
			var vectors [][]float32
			var err error
			for attempt := 0; attempt < 3; attempt++ {
				vectors, err = e.client.Embeddings(ctx, e.model, texts[start:end])
				if err == nil {
					break
				}
				time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
			}
			if err != nil {
				return nil, err
			}
			if len(vectors) != end-start {
				return nil, fmt.Errorf("向量数量不匹配")
			}
			for _, vector := range vectors {
				vector = e.normalize(vector)
				result = append(result, pgvector.NewVector(vector))
			}
		}
		return result, nil
	case "fastembed":
		return nil, fmt.Errorf("fastembed 暂未在 Go 版实现")
	default:
		return nil, fmt.Errorf("不支持的向量化提供方: %s", e.provider)
	}
}

func (e *Embedder) normalize(vector []float32) []float32 {
	if e.dimension <= 0 {
		return vector
	}
	if len(vector) == e.dimension {
		return vector
	}
	if len(vector) > e.dimension {
		return vector[:e.dimension]
	}
	out := make([]float32, e.dimension)
	copy(out, vector)
	return out
}

func (e *Embedder) mockEmbed(text string) []float32 {
	sum := md5.Sum([]byte(text))
	base := make([]float32, len(sum))
	for i, b := range sum {
		base[i] = float32(b) / 255.0
	}
	if e.dimension <= 0 {
		return base
	}
	out := make([]float32, e.dimension)
	for i := 0; i < e.dimension; i++ {
		out[i] = base[i%len(base)]
	}
	return out
}

const (
	embedCacheTTL        = 30 * time.Minute
	embedCacheMaxEntries = 1000
)

func (e *Embedder) cacheKey(text string) string {
	base := fmt.Sprintf("%s|%s|%d|%s", e.provider, e.model, e.dimension, text)
	return "embed:" + hashContent(base)
}

func (e *Embedder) getCachedVector(key string) ([]float32, bool) {
	if key == "" {
		return nil, false
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, ok := e.queryCache[key]
	if !ok {
		return nil, false
	}
	if entry.Expires.Before(now) {
		delete(e.queryCache, key)
		return nil, false
	}
	return cloneFloat32Slice(entry.Value), true
}

func (e *Embedder) setCachedVector(key string, value []float32) {
	if key == "" || len(value) == 0 {
		return
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.queryCache) >= embedCacheMaxEntries {
		pruneEmbedCache(e.queryCache, now)
	}
	e.queryCache[key] = cachedVector{
		Value:   cloneFloat32Slice(value),
		Expires: now.Add(embedCacheTTL),
	}
}

func pruneEmbedCache(cache map[string]cachedVector, now time.Time) {
	for key, entry := range cache {
		if entry.Expires.Before(now) {
			delete(cache, key)
		}
	}
	if len(cache) < embedCacheMaxEntries {
		return
	}
	target := embedCacheMaxEntries - embedCacheMaxEntries/10
	if target <= 0 {
		target = 1
	}
	for key := range cache {
		delete(cache, key)
		if len(cache) <= target {
			break
		}
	}
}

func cloneFloat32Slice(values []float32) []float32 {
	if len(values) == 0 {
		return nil
	}
	out := make([]float32, len(values))
	copy(out, values)
	return out
}
