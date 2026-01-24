package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestAutoRelativePathRules(t *testing.T) {
	path := autoRelativePath("对话", "dialogue_extract", "")
	if !strings.HasPrefix(path, "chat_history/") {
		t.Fatalf("dialogue_extract 路径错误: %s", path)
	}

	path = autoRelativePath("# 标题", "", "lesson")
	if !strings.HasPrefix(path, "lessons/") {
		t.Fatalf("lesson 路径错误: %s", path)
	}

	path = autoRelativePath("# 标题", "insight", "decision")
	if !strings.HasPrefix(path, "insights/") {
		t.Fatalf("insight 路径错误: %s", path)
	}
}

func TestSafeResolvePathTraversal(t *testing.T) {
	root := t.TempDir()
	_, _, _, err := safeResolvePath(root, "../evil.md")
	if err == nil {
		t.Fatalf("期望拒绝路径穿越")
	}
}

func TestEnsureFrontMatterRoundTrip(t *testing.T) {
	content := "# 标题\n\n内容"
	output := ensureFrontMatter(content, "insight", "decision", []string{"a", "b"})
	front, body := parseFrontMatter(output)
	if strings.TrimSpace(body) != content {
		t.Fatalf("正文不一致")
	}
	if front["knowledge_type"] != "insight" {
		t.Fatalf("knowledge_type 缺失")
	}
	if front["insight_type"] != "decision" {
		t.Fatalf("insight_type 缺失")
	}
	tagsRaw, ok := front["tags"].([]any)
	if !ok || len(tagsRaw) != 2 {
		t.Fatalf("tags 数量错误")
	}
}

func TestEmbedderMockDeterministic(t *testing.T) {
	settings := defaultSettings()
	settings.Embedding.Provider = "mock"
	settings.Embedding.Dimension = 32
	embedder := NewEmbedder(settings)

	vector1, err := embedder.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("向量化失败: %v", err)
	}
	vector2, err := embedder.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("向量化失败: %v", err)
	}
	if len(vector1.Slice()) != 32 {
		t.Fatalf("向量维度错误")
	}
	if !reflect.DeepEqual(vector1.Slice(), vector2.Slice()) {
		t.Fatalf("向量结果不一致")
	}
}

func TestNormalizeDatabaseURL(t *testing.T) {
	value := "postgresql+psycopg://user:pass@localhost:5432/db"
	normalized := normalizeDatabaseURL(value)
	if normalized != "postgresql://user:pass@localhost:5432/db" {
		t.Fatalf("数据库地址未归一化: %s", normalized)
	}
}
