package main

import (
	"context"
	"fmt"
	"strings"
)

type LLMClient struct {
	settings Settings
	client   *QwenClient
	mock     bool
}

type DistillResult struct {
	Summary      string
	InsightType  string
	Problem      string
	Thinking     []string
	Solution     string
	Result       []string
	Reproducible bool
	ApplicableTo []string
	Tags         []string
}

type Relation struct {
	Keyword      string
	RelationType string
}

func NewLLMClient(settings Settings) *LLMClient {
	mock := strings.ToLower(envOrDefault("AGENT_MEM_LLM_MODE", "")) == "mock"
	return &LLMClient{
		settings: settings,
		client:   NewQwenClient(settings),
		mock:     mock,
	}
}

func (l *LLMClient) DistillDialogue(content, projectID string) DistillResult {
	if l.mock {
		return l.mockDistill(content)
	}
	prompt := buildDistillPrompt(content, projectID)
	raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelDistill, prompt, 0.3, 2000)
	if err != nil {
		return l.mockDistill(content)
	}
	parsed := parseJSON(raw)
	if parsed == nil {
		return l.fallbackDistill(raw)
	}
	result := DistillResult{
		Summary:      getString(parsed, "summary"),
		InsightType:  getString(parsed, "insight_type"),
		Problem:      getString(parsed, "problem"),
		Thinking:     getStringSlice(parsed, "thinking"),
		Solution:     getString(parsed, "solution"),
		Result:       getStringSlice(parsed, "result"),
		Reproducible: getBool(parsed, "reproducible"),
		ApplicableTo: getStringSlice(parsed, "applicable_to"),
		Tags:         getStringSlice(parsed, "tags"),
	}
	if result.Summary == "" {
		result.Summary = "对话提炼"
	}
	return result
}

func (l *LLMClient) Summarize(content string) string {
	if l.mock {
		return mockSummary(content)
	}
	prompt := "请将以下文档内容压缩为 3-5 句摘要，突出核心结论。\n\n内容：\n" + truncate(content, 12000)
	raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelSummary, prompt, 0.2, 400)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

func (l *LLMClient) ExtractRelations(content string) []Relation {
	if l.mock {
		return []Relation{}
	}
	prompt := `从文档中提取可能引用或依赖的知识关键词，输出 JSON 数组：
[
  {"keyword": "内存泄漏复盘", "relation_type": "references"},
  {"keyword": "v2 需求文档", "relation_type": "based_on"}
]

允许的 relation_type: based_on / references / implements / validates / supersedes

文档：
` + truncate(content, 8000)
	raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelRelation, prompt, 0.2, 400)
	if err != nil {
		return []Relation{}
	}
	parsed := parseJSONArray(raw)
	if parsed == nil {
		return []Relation{}
	}
	var results []Relation
	for _, item := range parsed {
		keyword := strings.TrimSpace(getString(item, "keyword"))
		relationType := strings.TrimSpace(getString(item, "relation_type"))
		if keyword == "" || relationType == "" {
			continue
		}
		results = append(results, Relation{Keyword: keyword, RelationType: relationType})
	}
	return results
}

func (l *LLMClient) RouteQuery(query string) Route {
	intent := l.fallbackIntent(query)
	if !l.mock {
		prompt := `你是一个技术文档管理员。请根据用户问题的意图，将其精准分类为以下标签之一：

- decision: 涉及技术选型、架构决策、"为什么"、权衡对比 (e.g. "为什么选Go", "React vs Vue")
- debug: 涉及报错、故障排查、Bug修复 (e.g. "OOM怎么解", "Error: 500")
- howto: 涉及具体操作步骤、配置方法、部署流程 (e.g. "如何配置Nginx", "部署脚本")
- progress: 涉及项目进度、状态、任务清单 (e.g. "完成了多少", "本周计划")
- background: 纯粹的概念解释、需求描述、历史背景，且不包含上述特征 (e.g. "什么是MCP", "V1需求文档")

用户问题：` + query + `

只输出一个标签，不要包含其他文字。`

		raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelRoute, prompt, 0.0, 10)
		if err == nil {
			value := strings.TrimSpace(strings.ToLower(raw))
			// 移除所有标点
			value = strings.Map(func(r rune) rune {
				if r == '.' || r == '"' || r == '\'' || r == ',' {
					return -1
				}
				return r
			}, value)
			if value == "progress" || value == "decision" || value == "howto" || value == "debug" || value == "background" {
				intent = value
			}
		}
	}
	return l.routeFromIntent(intent)
}

func (l *LLMClient) ArbitrateConflict(newContent, oldContent string) string {
	if l.mock {
		return "supplement"
	}
	prompt := "判断下面两段文档的关系，只输出以下之一：\nreplace / supplement / conflict / unrelated\n\n旧文档：\n" + truncate(oldContent, 4000) + "\n\n新文档：\n" + truncate(newContent, 4000)
	raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelArbitrate, prompt, 0.0, 10)
	if err != nil {
		return "supplement"
	}
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "replace", "supplement", "conflict", "unrelated":
		return value
	default:
		return "supplement"
	}
}

func (l *LLMClient) Rerank(query string, documents []string, topN int) ([]RerankResult, error) {
	if l.mock {
		return nil, nil
	}
	if topN <= 0 {
		topN = 10
	}
	model := strings.TrimSpace(l.settings.Rerank.Model)
	if model == "" {
		return nil, fmt.Errorf("缺少 rerank 模型配置")
	}
	return l.client.Rerank(context.Background(), model, query, documents, topN)
}

func (l *LLMClient) fallbackIntent(query string) string {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "进度") || strings.Contains(q, "状态") || strings.Contains(q, "完成"):
		return "progress"
	case strings.Contains(q, "为什么") || strings.Contains(q, "选") || strings.Contains(q, "决策"):
		return "decision"
	case strings.Contains(q, "怎么") || strings.Contains(q, "如何") || strings.Contains(q, "部署"):
		return "howto"
	case strings.Contains(q, "bug") || strings.Contains(q, "报错") || strings.Contains(q, "排查") || strings.Contains(q, "错误"):
		return "debug"
	default:
		return "background"
	}
}

func (l *LLMClient) routeFromIntent(intent string) Route {
	routes := map[string]Route{
		"progress": {
			DocTypes:       []string{"progress", "issue"},
			MustLatest:     false,
			TimeFilterDays: intPtr(3),
			OrderBy:        "time_desc",
		},
		"decision": {
			DocTypes:       []string{"architecture", "insight", "background", "dialogue_extract"},
			MustLatest:     false,
			TimeFilterDays: nil,
			OrderBy:        "relevance",
		},
		"howto": {
			DocTypes:       []string{"deployment", "delivery", "implementation"},
			MustLatest:     true,
			TimeFilterDays: nil,
			OrderBy:        "relevance",
		},
		"debug": {
			DocTypes:       []string{"issue", "progress", "insight"},
			MustLatest:     false,
			TimeFilterDays: nil,
			OrderBy:        "relevance",
		},
		"background": {
			DocTypes:       []string{"background", "architecture"},
			MustLatest:     false,
			TimeFilterDays: nil,
			OrderBy:        "relevance",
		},
	}
	if route, ok := routes[intent]; ok {
		return route
	}
	return routes["background"]
}

func (l *LLMClient) mockDistill(content string) DistillResult {
	return DistillResult{
		Summary:      "对话提炼(模拟)",
		InsightType:  "solution",
		Problem:      "",
		Thinking:     []string{},
		Solution:     truncate(content, 2000),
		Result:       []string{},
		Reproducible: false,
		ApplicableTo: []string{},
		Tags:         []string{},
	}
}

func (l *LLMClient) fallbackDistill(raw string) DistillResult {
	return DistillResult{
		Summary:      "对话提炼失败",
		InsightType:  "solution",
		Problem:      "",
		Thinking:     []string{},
		Solution:     truncate(raw, 2000),
		Result:       []string{},
		Reproducible: false,
		ApplicableTo: []string{},
		Tags:         []string{},
	}
}

func buildDistillPrompt(content, projectID string) string {
	return `你是资深技术负责人。以下是项目 ` + projectID + ` 的对话记录，请忽略寒暄和试错，提炼出结构化干货。

重要约束：仅基于提供的对话内容作答。严禁编造原文未提及的细节（如版本号、协议细节等）。如果原文未提及，请留空。

请输出严格 JSON，格式如下：
{
  "summary": "一句话摘要",
  "insight_type": "solution|lesson|pattern|decision",
  "problem": "问题/挑战",
  "thinking": ["思考要点1", "思考要点2"],
  "solution": "最终方案",
  "result": ["结果1", "结果2"],
  "reproducible": true,
  "applicable_to": ["场景A", "场景B"],
  "tags": ["标签1", "标签2"]
}

对话内容：
` + truncate(content, 15000)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func mockSummary(content string) string {
	lines := strings.Split(content, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, "；")
}
