package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultHost      = "127.0.0.1"
	defaultPort      = 8787
	defaultProjectID = "global"
)

type Settings struct {
	Project    ProjectConfig    `yaml:"project"`
	Watcher    WatcherConfig    `yaml:"watcher"`
	Versioning VersioningConfig `yaml:"versioning"`
	LLM        LLMConfig        `yaml:"llm"`
	Embedding  EmbeddingConfig  `yaml:"embedding"`
	Rerank     RerankConfig     `yaml:"rerank"`
	Storage    StorageConfig    `yaml:"storage"`
}

type ProjectConfig struct {
	RootMarkers      []string `yaml:"root_markers"`
	ProjectIDKey     string   `yaml:"project_id_key"`
	ProjectNameKey   string   `yaml:"project_name_key"`
	DefaultProjectID string   `yaml:"default_project_id"`
}

type WatcherConfig struct {
	Roots         []string `yaml:"roots"`
	MaxFileSizeKB int      `yaml:"max_file_size_kb"`
	WatchDirs     []string `yaml:"watch_dirs"`
	WatchRoot     []string `yaml:"watch_root_files"`
	Extensions    []string `yaml:"extensions"`
	IgnoreDirs    []string `yaml:"ignore_dirs"`
	ExtraRoots    []string `yaml:"extra_roots"`
}

type VersioningConfig struct {
	SemanticSimilarityThreshold float64 `yaml:"semantic_similarity_threshold"`
}

type LLMConfig struct {
	BaseURL        string `yaml:"base_url"`
	APIKeyEnv      string `yaml:"api_key_env"`
	ModelDistill   string `yaml:"model_distill"`
	ModelClassify  string `yaml:"model_classify"`
	ModelRoute     string `yaml:"model_route"`
	ModelRelation  string `yaml:"model_relation"`
	ModelArbitrate string `yaml:"model_arbitrate"`
	ModelSummary   string `yaml:"model_summary"`
}

type EmbeddingConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	Dimension int    `yaml:"dimension"`
	BatchSize int    `yaml:"batch_size"`
}

type RerankConfig struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
	TopN    int    `yaml:"top_n"`
}

type StorageConfig struct {
	DatabaseURL string `yaml:"database_url"`
}

func defaultSettings() Settings {
	return Settings{
		Project: ProjectConfig{
			RootMarkers:      []string{".git", ".project.yaml", "package.json", "pyproject.toml", "Cargo.toml", "go.mod"},
			ProjectIDKey:     "project_id",
			ProjectNameKey:   "project_name",
			DefaultProjectID: defaultProjectID,
		},
		Watcher: WatcherConfig{
			Roots:         []string{},
			MaxFileSizeKB: 2048,
			WatchDirs: []string{
				"docs", "doc", "specs", "requirements", "progress", "notes", "design",
				"architecture", "insights", "lessons", "postmortem", "chat_history",
			},
			WatchRoot: []string{
				"README.md", "README.txt", "TASKS.md", "CHANGELOG.md", "TODO.md", "NOTES.md", "DESIGN.md", "ARCHITECTURE.md",
			},
			Extensions: []string{".md", ".txt", ".rst", ".adoc", ".org", ".yaml", ".yml", ".json"},
			IgnoreDirs: []string{".git", "node_modules", "__pycache__", ".venv", "venv", "env", "dist", "build", "target", ".idea", ".vscode", ".pytest_cache"},
			ExtraRoots: []string{},
		},
		Versioning: VersioningConfig{
			SemanticSimilarityThreshold: 0.85,
		},
		LLM: LLMConfig{
			BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKeyEnv:      "DASHSCOPE_API_KEY",
			ModelDistill:   "qwen-plus",
			ModelClassify:  "qwen-turbo",
			ModelRoute:     "qwen-turbo",
			ModelRelation:  "qwen-turbo",
			ModelArbitrate: "qwen-flash",
			ModelSummary:   "qwen-turbo",
		},
		Embedding: EmbeddingConfig{Provider: "qwen", Model: "text-embedding-v4", Dimension: 1024, BatchSize: 32},
		Rerank:    RerankConfig{Enabled: false, Model: "gte-rerank-v2", TopN: 10},
		Storage:   StorageConfig{DatabaseURL: "postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge"},
	}
}

func loadSettings(configPath string) (Settings, error) {
	loadEnvFile(envOrDefault("AGENT_TOOLS_ENV", "~/.config/agent_tools.env"))

	settings := defaultSettings()
	resolved := resolveConfigPath(configPath)
	if resolved != "" {
		data, err := os.ReadFile(resolved)
		if err != nil {
			return settings, err
		}
		if err := yaml.Unmarshal(data, &settings); err != nil {
			return settings, err
		}
	}

	if envDB := os.Getenv("DATABASE_URL"); envDB != "" {
		settings.Storage.DatabaseURL = envDB
	}
	if envBase := os.Getenv("DASHSCOPE_BASE_URL"); envBase != "" {
		settings.LLM.BaseURL = envBase
	}
	if envKey := os.Getenv("DASHSCOPE_API_KEY"); envKey != "" && settings.LLM.APIKeyEnv == "" {
		settings.LLM.APIKeyEnv = "DASHSCOPE_API_KEY"
	}
	if envProvider := os.Getenv("AGENT_MEM_EMBEDDING_PROVIDER"); envProvider != "" {
		settings.Embedding.Provider = envProvider
	}
	if envModel := os.Getenv("AGENT_MEM_EMBEDDING_MODEL"); envModel != "" {
		settings.Embedding.Model = envModel
	}
	if envDim := os.Getenv("AGENT_MEM_EMBEDDING_DIMENSION"); envDim != "" {
		if value, err := strconv.Atoi(envDim); err == nil && value > 0 {
			settings.Embedding.Dimension = value
		}
	}
	settings.Storage.DatabaseURL = normalizeDatabaseURL(settings.Storage.DatabaseURL)
	return settings, nil
}

func resolveConfigPath(configPath string) string {
	if configPath != "" {
		return configPath
	}
	if envPath := os.Getenv("AGENT_MEM_CONFIG"); envPath != "" {
		return envPath
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	current := cwd
	for {
		candidate := filepath.Join(current, "config", "settings.yaml")
		if exists(candidate) {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func loadEnvFile(path string) {
	resolved := expandHome(path)
	data, err := os.ReadFile(resolved)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, "`'\"")
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, os.ExpandEnv(value))
	}
}

func normalizeDatabaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "postgresql+") {
		if idx := strings.Index(value, "://"); idx != -1 {
			return "postgresql://" + value[idx+3:]
		}
	}
	return value
}