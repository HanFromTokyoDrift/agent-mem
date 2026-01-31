package main

import "testing"

func TestRRFMergeWithTrace(t *testing.T) {
	rowsA := []FragmentRow{
		{FragmentID: "f1", Ts: 2},
		{FragmentID: "f2", Ts: 1},
	}
	rowsB := []FragmentRow{
		{FragmentID: "f2", Ts: 1},
		{FragmentID: "f3", Ts: 3},
	}
	_, trace := rrfMergeWithTrace(
		SourceRows{Name: "vector", Rows: rowsA},
		SourceRows{Name: "bm25", Rows: rowsB},
	)
	item, ok := trace["f2"]
	if !ok {
		t.Fatalf("trace 缺少 f2")
	}
	if item.Ranks["vector"] != 2 || item.Ranks["bm25"] != 1 {
		t.Fatalf("trace 排名错误: %+v", item.Ranks)
	}
	if !stringInSlice(item.Sources, "vector") || !stringInSlice(item.Sources, "bm25") {
		t.Fatalf("trace 来源缺失: %+v", item.Sources)
	}
	if item.RRFScore <= 0 {
		t.Fatalf("RRF 分数未设置: %+v", item.RRFScore)
	}
}
