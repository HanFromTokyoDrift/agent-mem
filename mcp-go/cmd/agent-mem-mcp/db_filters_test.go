package main

import (
	"strings"
	"testing"
)

func TestAppendIndexPathFilterPrefix(t *testing.T) {
	query, args := appendIndexPathFilter("SELECT 1 WHERE true", nil, []string{"alpha", "beta"})
	if len(args) != 2 || args[0] != "alpha" || args[1] != "beta" {
		t.Fatalf("index_path 参数错误: %+v", args)
	}
	if !strings.Contains(query, "index_path->>0") || !strings.Contains(query, "index_path->>1") {
		t.Fatalf("index_path 前缀条件缺失: %s", query)
	}
}

func TestAppendIndexPathWhere(t *testing.T) {
	where, args := appendIndexPathWhere("p.owner_id = $2", []any{20, "owner"}, []string{"root", "child"})
	if len(args) != 4 || args[2] != "root" || args[3] != "child" {
		t.Fatalf("where 参数错误: %+v", args)
	}
	if !strings.Contains(where, "index_path->>0") || !strings.Contains(where, "index_path->>1") {
		t.Fatalf("where 前缀条件缺失: %s", where)
	}
}
