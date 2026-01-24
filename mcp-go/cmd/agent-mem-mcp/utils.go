package main

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, tag := range tags {
		value := strings.TrimSpace(tag)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func ensureFrontMatter(content, knowledgeType, insightType string, tags []string) string {
	trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
	if strings.HasPrefix(trimmed, "---") {
		return content
	}
	if knowledgeType == "" && insightType == "" && len(tags) == 0 {
		return content
	}
	front := buildFrontMatter(knowledgeType, insightType, tags)
	return front + "\n" + strings.TrimSpace(content) + "\n"
}

func buildFrontMatter(knowledgeType, insightType string, tags []string) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	if knowledgeType != "" {
		builder.WriteString("knowledge_type: ")
		builder.WriteString(knowledgeType)
		builder.WriteString("\n")
	}
	if insightType != "" {
		builder.WriteString("insight_type: ")
		builder.WriteString(insightType)
		builder.WriteString("\n")
	}
	if len(tags) > 0 {
		builder.WriteString("tags:\n")
		for _, tag := range tags {
			builder.WriteString("  - ")
			builder.WriteString(tag)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("---")
	return builder.String()
}

func parseFrontMatter(content string) (map[string]any, string) {
	if !strings.HasPrefix(content, "---") {
		return map[string]any{}, content
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return map[string]any{}, content
	}
	raw := strings.TrimSpace(parts[1])
	body := strings.TrimLeft(parts[2], "\n")
	if raw == "" {
		return map[string]any{}, body
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		return map[string]any{}, content
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, body
}

func extractTitle(content, fallback string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}

func slugify(text string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(text) {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if r == ' ' || r == '-' || r == '_' {
			if !lastDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func autoRelativePath(content, knowledgeType, insightType string) string {
	front, body := parseFrontMatter(content)
	if knowledgeType == "" {
		if v, ok := front["knowledge_type"].(string); ok {
			knowledgeType = strings.TrimSpace(v)
		}
	}
	if insightType == "" {
		if v, ok := front["insight_type"].(string); ok {
			insightType = strings.TrimSpace(v)
		}
	}

	knowledgeType = strings.ToLower(knowledgeType)
	insightType = strings.ToLower(insightType)

	baseDir := "notes"
	if knowledgeType == "dialogue_extract" {
		baseDir = "chat_history"
	} else if insightType == "lesson" {
		baseDir = "lessons"
	} else if insightType == "solution" || insightType == "pattern" || insightType == "decision" || knowledgeType == "insight" {
		baseDir = "insights"
	}

	title := extractTitle(body, "memory")
	slug := slugify(title)
	if slug == "" {
		slug = "memory"
	}
	stamp := time.Now().UTC().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s_%s.md", stamp, slug)
	return filepath.ToSlash(filepath.Join(baseDir, filename))
}

func safeResolvePath(projectRoot, relativePath string) (string, string, string, error) {
	root := expandHome(projectRoot)
	if root == "" {
		return "", "", "", fmt.Errorf("project_root 无效")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(rootAbs, 0o755); err != nil {
		return "", "", "", err
	}

	if filepath.IsAbs(relativePath) {
		return "", "", "", fmt.Errorf("relative_path 必须是相对路径")
	}
	rel := filepath.Clean(relativePath)
	if rel == "." {
		rel = defaultFilename()
	}

	if strings.HasSuffix(relativePath, "/") || strings.HasSuffix(relativePath, string(filepath.Separator)) {
		rel = filepath.Join(rel, defaultFilename())
	}

	targetAbs := filepath.Join(rootAbs, rel)
	targetAbs = filepath.Clean(targetAbs)

	if !isWithin(rootAbs, targetAbs) {
		return "", "", "", fmt.Errorf("relative_path 超出 project_root 范围")
	}

	if info, err := os.Stat(targetAbs); err == nil && info.IsDir() {
		targetAbs = filepath.Join(targetAbs, defaultFilename())
	}
	if filepath.Ext(targetAbs) == "" {
		targetAbs += ".md"
	}

	relPath, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", "", "", err
	}
	return rootAbs, targetAbs, relPath, nil
}

func defaultFilename() string {
	stamp := time.Now().UTC().Format("2006-01-02_15-04-05")
	return fmt.Sprintf("memory_%s.md", stamp)
}

func appendSuffix(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	suffix := time.Now().UTC().Format("20060102150405")
	return fmt.Sprintf("%s_%s%s", base, suffix, ext)
}

func isWithin(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == target {
		return true
	}
	rootWithSep := root + string(filepath.Separator)
	return strings.HasPrefix(target, rootWithSep)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

func ensureDir(path string) error {
	if path == "" {
		return fmt.Errorf("目录为空")
	}
	return os.MkdirAll(path, 0o755)
}

func writeText(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func readFileSafe(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	// latin-1 fallback
	out := make([]rune, len(data))
	for i, b := range data {
		out[i] = rune(b)
	}
	return string(out), nil
}

func calculateFileHash(content string) string {
	sum := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", sum)
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
