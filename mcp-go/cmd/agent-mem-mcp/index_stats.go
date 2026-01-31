package main

import "sort"

type MemoryCounts struct {
	Total     int
	Axes      int
	IndexPath int
}

func buildIndexStats(counts MemoryCounts, depthDist []DepthCount, tree []IndexPathNode, prefixLen int) IndexStats {
	stats := IndexStats{
		TotalMemories:     counts.Total,
		AxesCoverage:      ratio(counts.Axes, counts.Total),
		IndexPathCoverage: ratio(counts.IndexPath, counts.Total),
		BranchingFactor:   computeBranchingFactor(tree),
	}
	adjusted := adjustDepthDistribution(depthDist, prefixLen)
	stats.DepthDistribution = adjusted
	if len(adjusted) == 0 {
		return stats
	}
	var totalDepth int
	var totalCount int
	maxDepth := 0
	for _, item := range adjusted {
		totalDepth += item.Depth * item.Count
		totalCount += item.Count
		if item.Depth > maxDepth {
			maxDepth = item.Depth
		}
	}
	if totalCount > 0 {
		stats.AvgPathDepth = float64(totalDepth) / float64(totalCount)
		stats.MaxPathDepth = maxDepth
	}
	return stats
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func adjustDepthDistribution(input []DepthCount, prefixLen int) []DepthCount {
	if len(input) == 0 {
		return nil
	}
	if prefixLen < 0 {
		prefixLen = 0
	}
	agg := map[int]int{}
	for _, item := range input {
		depth := item.Depth - prefixLen
		if depth < 0 {
			continue
		}
		agg[depth] += item.Count
	}
	if len(agg) == 0 {
		return nil
	}
	keys := make([]int, 0, len(agg))
	for depth := range agg {
		keys = append(keys, depth)
	}
	sort.Ints(keys)
	result := make([]DepthCount, 0, len(keys))
	for _, depth := range keys {
		result = append(result, DepthCount{Depth: depth, Count: agg[depth]})
	}
	return result
}

func computeBranchingFactor(tree []IndexPathNode) float64 {
	if len(tree) == 0 {
		return 0
	}
	nodes, children := countTreeNodes(tree)
	if nodes == 0 {
		return 0
	}
	return float64(children) / float64(nodes)
}

func countTreeNodes(nodes []IndexPathNode) (int, int) {
	totalNodes := 0
	totalChildren := 0
	for _, node := range nodes {
		totalNodes++
		childCount := len(node.Children)
		totalChildren += childCount
		childNodes, childChildren := countTreeNodes(node.Children)
		totalNodes += childNodes
		totalChildren += childChildren
	}
	return totalNodes, totalChildren
}
