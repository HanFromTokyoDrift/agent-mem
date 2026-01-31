package main

import (
	"testing"
	"time"
)

func TestLLMCacheTextHit(t *testing.T) {
	client := NewLLMClient(defaultSettings())
	client.setCachedText(client.summaryCache, "k1", "v1")
	value, ok := client.getCachedText(client.summaryCache, "k1")
	if !ok || value != "v1" {
		t.Fatalf("文本缓存命中失败: %v %s", ok, value)
	}
}

func TestLLMCacheTextExpired(t *testing.T) {
	client := NewLLMClient(defaultSettings())
	client.summaryCache["k2"] = cachedText{Value: "v2", Expires: time.Now().Add(-time.Minute)}
	if _, ok := client.getCachedText(client.summaryCache, "k2"); ok {
		t.Fatalf("过期文本缓存未失效")
	}
	if _, ok := client.summaryCache["k2"]; ok {
		t.Fatalf("过期文本缓存未清理")
	}
}

func TestLLMCacheTagsClone(t *testing.T) {
	client := NewLLMClient(defaultSettings())
	client.setCachedTags(client.tagsCache, "k3", []string{"a", "b"})
	first, ok := client.getCachedTags(client.tagsCache, "k3")
	if !ok || len(first) == 0 {
		t.Fatalf("标签缓存读取失败")
	}
	first[0] = "x"
	second, ok := client.getCachedTags(client.tagsCache, "k3")
	if !ok || second[0] == "x" {
		t.Fatalf("标签缓存未进行拷贝隔离")
	}
}

func TestLLMCacheIndexClone(t *testing.T) {
	client := NewLLMClient(defaultSettings())
	axes := MemoryAxes{Domain: []string{"a"}}
	path := []string{"p"}
	client.setCachedIndex("k4", axes, path)
	first, ok := client.getCachedIndex("k4")
	if !ok {
		t.Fatalf("索引缓存读取失败")
	}
	first.Axes.Domain[0] = "x"
	first.Path[0] = "y"
	second, ok := client.getCachedIndex("k4")
	if !ok || second.Axes.Domain[0] != "a" || second.Path[0] != "p" {
		t.Fatalf("索引缓存未进行拷贝隔离")
	}
}
