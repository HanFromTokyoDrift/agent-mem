package main

import "testing"

func TestBuildIndexStats(t *testing.T) {
	counts := MemoryCounts{Total: 4, Axes: 2, IndexPath: 3}
	depthDist := []DepthCount{
		{Depth: 2, Count: 2},
		{Depth: 3, Count: 1},
	}
	tree := []IndexPathNode{
		{
			Name:  "root",
			Count: 3,
			Children: []IndexPathNode{
				{Name: "a", Count: 2},
				{Name: "b", Count: 1},
			},
		},
	}
	stats := buildIndexStats(counts, depthDist, tree, 1)
	if stats.TotalMemories != 4 {
		t.Fatalf("total 错误: %d", stats.TotalMemories)
	}
	if stats.AxesCoverage != 0.5 {
		t.Fatalf("axes 覆盖率错误: %f", stats.AxesCoverage)
	}
	if stats.IndexPathCoverage != 0.75 {
		t.Fatalf("index_path 覆盖率错误: %f", stats.IndexPathCoverage)
	}
	if stats.MaxPathDepth != 2 {
		t.Fatalf("最大深度错误: %d", stats.MaxPathDepth)
	}
	if stats.AvgPathDepth < 1.3 || stats.AvgPathDepth > 1.35 {
		t.Fatalf("平均深度错误: %f", stats.AvgPathDepth)
	}
	if stats.BranchingFactor < 0.6 || stats.BranchingFactor > 0.7 {
		t.Fatalf("分支因子错误: %f", stats.BranchingFactor)
	}
	if len(stats.DepthDistribution) != 2 {
		t.Fatalf("深度分布数量错误: %+v", stats.DepthDistribution)
	}
}
