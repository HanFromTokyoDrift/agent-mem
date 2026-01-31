package main

import (
	"testing"
	"time"
)

func TestEmbedderCacheHit(t *testing.T) {
	settings := defaultSettings()
	settings.Embedding.Provider = "mock"
	settings.Embedding.Dimension = 3
	embedder := NewEmbedder(settings)

	key := embedder.cacheKey("hello")
	embedder.setCachedVector(key, []float32{0.11, 0.22, 0.33})
	vector, err := embedder.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("向量化失败: %v", err)
	}
	if !float32SliceEqual(vector.Slice(), []float32{0.11, 0.22, 0.33}) {
		t.Fatalf("未命中缓存: %+v", vector.Slice())
	}
}

func TestEmbedderCacheExpired(t *testing.T) {
	settings := defaultSettings()
	settings.Embedding.Provider = "mock"
	embedder := NewEmbedder(settings)

	key := embedder.cacheKey("expire")
	embedder.queryCache[key] = cachedVector{
		Value:   []float32{0.1},
		Expires: time.Now().Add(-time.Minute),
	}
	if _, ok := embedder.getCachedVector(key); ok {
		t.Fatalf("过期缓存未失效")
	}
	if _, ok := embedder.queryCache[key]; ok {
		t.Fatalf("过期缓存未清理")
	}
}

func TestEmbedderCacheClone(t *testing.T) {
	settings := defaultSettings()
	settings.Embedding.Provider = "mock"
	embedder := NewEmbedder(settings)

	key := embedder.cacheKey("clone")
	embedder.setCachedVector(key, []float32{0.5, 0.6})
	first, ok := embedder.getCachedVector(key)
	if !ok || len(first) == 0 {
		t.Fatalf("读取缓存失败")
	}
	first[0] = 9.9
	second, ok := embedder.getCachedVector(key)
	if !ok || second[0] == 9.9 {
		t.Fatalf("缓存未进行拷贝隔离")
	}
}

func float32SliceEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
