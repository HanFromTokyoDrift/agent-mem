package main

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

type MetricsCache struct {
	mu         sync.Mutex
	entries    map[string]cachedMetrics
	ttl        time.Duration
	maxEntries int
}

type cachedMetrics struct {
	Value   MetricsResponse
	Expires time.Time
}

const (
	defaultMetricsCacheTTL        = 15 * time.Second
	defaultMetricsCacheMaxEntries = 200
)

func NewMetricsCache() *MetricsCache {
	ttl := defaultMetricsCacheTTL
	if raw := strings.TrimSpace(envOrDefault("AGENT_MEM_METRICS_CACHE_TTL", "")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			ttl = time.Duration(value) * time.Second
		}
	}
	maxEntries := defaultMetricsCacheMaxEntries
	if raw := strings.TrimSpace(envOrDefault("AGENT_MEM_METRICS_CACHE_MAX", "")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			maxEntries = value
		}
	}
	return &MetricsCache{
		entries:    map[string]cachedMetrics{},
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

func (c *MetricsCache) Get(key string) (MetricsResponse, bool) {
	if c == nil || key == "" {
		return MetricsResponse{}, false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return MetricsResponse{}, false
	}
	if entry.Expires.Before(now) {
		delete(c.entries, key)
		return MetricsResponse{}, false
	}
	return entry.Value, true
}

func (c *MetricsCache) Set(key string, value MetricsResponse) {
	if c == nil || key == "" || value.Content == "" {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxEntries {
		c.prune(now)
	}
	c.entries[key] = cachedMetrics{
		Value:   value,
		Expires: now.Add(c.ttl),
	}
}

func (c *MetricsCache) prune(now time.Time) {
	for key, entry := range c.entries {
		if entry.Expires.Before(now) {
			delete(c.entries, key)
		}
	}
	if len(c.entries) < c.maxEntries {
		return
	}
	target := c.maxEntries - c.maxEntries/10
	if target <= 0 {
		target = 1
	}
	for key := range c.entries {
		delete(c.entries, key)
		if len(c.entries) <= target {
			break
		}
	}
}
