package main

import "testing"

func TestBuildIndexPathTree(t *testing.T) {
	paths := []IndexPathCount{
		{Path: []string{"alpha", "beta"}, Count: 2},
		{Path: []string{"alpha", "gamma"}, Count: 1},
		{Path: []string{"zeta"}, Count: 3},
		{Path: []string{}, Count: 5},
	}
	tree := buildIndexPathTree(paths, 0, 0)
	if len(tree) != 2 {
		t.Fatalf("顶层节点数量错误: %d", len(tree))
	}
	if tree[0].Name != "alpha" || tree[0].Count != 3 {
		t.Fatalf("alpha 聚合错误: %+v", tree[0])
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("alpha 子节点数量错误: %d", len(tree[0].Children))
	}
	if tree[0].Children[0].Name != "beta" || tree[0].Children[0].Count != 2 {
		t.Fatalf("beta 计数错误: %+v", tree[0].Children[0])
	}
	if tree[1].Name != "zeta" || tree[1].Count != 3 {
		t.Fatalf("zeta 计数错误: %+v", tree[1])
	}
}

func TestBuildIndexPathTreeDepthLimit(t *testing.T) {
	paths := []IndexPathCount{
		{Path: []string{"root", "child", "leaf"}, Count: 2},
	}
	tree := buildIndexPathTree(paths, 1, 0)
	if len(tree) != 1 || tree[0].Name != "root" {
		t.Fatalf("深度裁剪失败: %+v", tree)
	}
	if len(tree[0].Children) != 0 {
		t.Fatalf("深度裁剪未生效: %+v", tree[0].Children)
	}
}

func TestBuildIndexPathTreeWidthLimit(t *testing.T) {
	paths := []IndexPathCount{
		{Path: []string{"root", "b"}, Count: 1},
		{Path: []string{"root", "a"}, Count: 2},
		{Path: []string{"root", "c"}, Count: 3},
	}
	tree := buildIndexPathTree(paths, 0, 2)
	if len(tree) != 1 {
		t.Fatalf("顶层节点数量错误: %d", len(tree))
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("宽度裁剪失败: %+v", tree[0].Children)
	}
	if tree[0].Children[0].Name != "c" {
		t.Fatalf("宽度排序错误: %+v", tree[0].Children)
	}
}

func TestTrimIndexPathCounts(t *testing.T) {
	paths := []IndexPathCount{
		{Path: []string{"root", "alpha"}, Count: 2},
		{Path: []string{"root", "beta"}, Count: 1},
		{Path: []string{"other", "x"}, Count: 3},
		{Path: []string{"root"}, Count: 4},
	}
	trimmed := trimIndexPathCounts(paths, []string{"root"})
	if len(trimmed) != 2 {
		t.Fatalf("trim 数量错误: %+v", trimmed)
	}
	if trimmed[0].Path[0] != "alpha" && trimmed[1].Path[0] != "alpha" {
		t.Fatalf("trim 结果缺少 alpha: %+v", trimmed)
	}
}
