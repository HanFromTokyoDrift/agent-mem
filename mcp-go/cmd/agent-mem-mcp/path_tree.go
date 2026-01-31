package main

import (
	"sort"
	"strings"
)

type pathTreeNode struct {
	name     string
	count    int
	children map[string]*pathTreeNode
}

func buildIndexPathTree(paths []IndexPathCount, maxDepth, maxChildren int) []IndexPathNode {
	if len(paths) == 0 {
		return nil
	}
	root := &pathTreeNode{children: map[string]*pathTreeNode{}}
	for _, item := range paths {
		if len(item.Path) == 0 || item.Count <= 0 {
			continue
		}
		current := root
		for idx, part := range item.Path {
			if maxDepth > 0 && idx >= maxDepth {
				break
			}
			if part == "" {
				continue
			}
			child := current.children[part]
			if child == nil {
				child = &pathTreeNode{name: part, children: map[string]*pathTreeNode{}}
				current.children[part] = child
			}
			child.count += item.Count
			current = child
		}
	}
	return buildPathNodes(root, maxChildren)
}

func buildPathNodes(node *pathTreeNode, maxChildren int) []IndexPathNode {
	if node == nil || len(node.children) == 0 {
		return nil
	}
	nodes := make([]IndexPathNode, 0, len(node.children))
	for _, child := range node.children {
		nodes = append(nodes, IndexPathNode{
			Name:     child.name,
			Count:    child.count,
			Children: buildPathNodes(child, maxChildren),
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Count == nodes[j].Count {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].Count > nodes[j].Count
	})
	if maxChildren > 0 && len(nodes) > maxChildren {
		nodes = nodes[:maxChildren]
	}
	return nodes
}

func trimIndexPathCounts(paths []IndexPathCount, prefix []string) []IndexPathCount {
	if len(prefix) == 0 {
		return paths
	}
	var trimmed []IndexPathCount
	for _, item := range paths {
		if len(item.Path) < len(prefix) {
			continue
		}
		if !indexPathHasPrefix(item.Path, prefix) {
			continue
		}
		rest := item.Path[len(prefix):]
		if len(rest) == 0 {
			continue
		}
		trimmed = append(trimmed, IndexPathCount{Path: rest, Count: item.Count})
	}
	return trimmed
}

func indexPathHasPrefix(path, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(path) < len(prefix) {
		return false
	}
	for i, item := range prefix {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if path[i] != value {
			return false
		}
	}
	return true
}
