package main

import (
	"encoding/json"
	"strings"
)

const (
	maxAxisValues          = 20
	maxAxisValueLen        = 80
	maxIndexPathDepth      = 10
	maxIndexPathSegmentLen = 120
)

func normalizeAxesInput(input *MemoryAxes) *MemoryAxes {
	if input == nil {
		return nil
	}
	normalized := MemoryAxes{
		Domain:    normalizeAxisValues(input.Domain),
		Stack:     normalizeAxisValues(input.Stack),
		Problem:   normalizeAxisValues(input.Problem),
		Lifecycle: normalizeAxisValues(input.Lifecycle),
		Component: normalizeAxisValues(input.Component),
	}
	if axesEmpty(normalized) {
		return nil
	}
	return &normalized
}

func normalizeAxisValues(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		item = strings.ToLower(item)
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func normalizeIndexPath(values []string) []string {
	var result []string
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		item = strings.ToLower(item)
		result = append(result, item)
	}
	return result
}

func axesEmpty(axes MemoryAxes) bool {
	return len(axes.Domain) == 0 &&
		len(axes.Stack) == 0 &&
		len(axes.Problem) == 0 &&
		len(axes.Lifecycle) == 0 &&
		len(axes.Component) == 0
}

func axesPtr(axes MemoryAxes) *MemoryAxes {
	if axesEmpty(axes) {
		return nil
	}
	copyAxes := axes
	return &copyAxes
}

func decodeAxes(raw []byte) MemoryAxes {
	if len(raw) == 0 {
		return MemoryAxes{}
	}
	var axes MemoryAxes
	if err := json.Unmarshal(raw, &axes); err != nil {
		return MemoryAxes{}
	}
	normalized := normalizeAxesInput(&axes)
	if normalized == nil {
		return MemoryAxes{}
	}
	return *normalized
}

func decodeIndexPath(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var path []string
	if err := json.Unmarshal(raw, &path); err != nil {
		return []string{}
	}
	return normalizeIndexPath(path)
}

func nullableJSON(raw []byte, empty bool) any {
	if empty || len(raw) == 0 {
		return nil
	}
	return string(raw)
}
