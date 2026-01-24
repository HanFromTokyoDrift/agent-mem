# Agent Memory (Project Cortex)

> 🧠 **赋予 AI Agent 长期记忆与自我进化能力**
>
> 一个基于 **Go** 实现的轻量级、高性能知识库中间件。支持文件系统实时监控、智能语义入库、自动版本仲裁与 MCP 协议。

---

## ✨ 核心特性

*   **⚡ 极速架构**: 纯 Go 实现 (Watcher + Ingester + Server)，单二进制文件，资源占用极低。
*   **👁️ 实时感知**: 基于 `fsnotify` 监控本地目录，文档变更毫秒级入库。
*   **🧠 认知智能**:
    *   **意图路由**: 自动识别 Debug / Howto / Decision 等查询意图。
    *   **对话炼金**: 从杂乱的 Chat Log 中提炼结构化 Solution / Lesson。
    *   **版本仲裁**: 智能判断新旧知识关系 (Replace / Supplement)，保持知识库的“唯一真理”。
*   **🔌 标准接口**: 原生支持 **Model Context Protocol (MCP)**，无缝对接 Claude Desktop, Cursor, Gemini CLI。
*   **🧹 极客哲学**: 默认支持“硬删除”模式，旧知识直接物理抹除，拒绝数据膨胀。

## 🏗️ 架构概览

```mermaid
graph LR
    File[📝 Markdown/Logs] --> Watcher[👀 Go Watcher]
    Watcher --> Ingester[⚙️ Ingest Pipeline]
    
    subgraph "Core Logic"
        Ingester --> Classifier[🏷️ Classify]
        Classifier --> Distiller[⚗️ Distill (Qwen)]
        Distiller --> Embedder[📐 Vectorize]
        Embedder --> Arbiter[⚖️ Arbitrate]
    end
    
    Arbiter --> DB[(🐘 PostgreSQL + pgvector)]
    
    User[🤖 Claude/Cursor] -- MCP Protocol --> Server[🚀 MCP Server]
    Server --> DB
```

## 🚀 快速开始

### 1. 依赖准备

确保已安装 [Docker](https://www.docker.com/) 和 [Go 1.25+](https://go.dev/)。

```bash
# 启动 PostgreSQL (带 pgvector 扩展)
docker-compose up -d
```

### 2. 配置环境

复制模版并填入您的 API Key (目前深度适配 Aliyun Qwen 模型)：

```bash
cp .env.example .env
vim .env
```

```env
DASHSCOPE_API_KEY=sk-xxxxxxxxxxxx
DATABASE_URL=postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge
```

### 3. 编译与运行

```bash
# 编译
cd mcp-go
go mod tidy
go build -o ../agent-mem ./cmd/agent-mem-mcp

# 回到根目录运行 (同时开启监控和HTTP服务)
cd ..
./agent-mem --watch --transport http
```

## ⚙️ 配置说明

核心策略在 `config/settings.yaml` 中定义：

```yaml
watcher:
  # 监控目录 (相对于运行目录)
  watch_dirs: ["docs", "notes", "insights", "chat_history"]
  # 忽略规则
  ignore_dirs: [".git", "node_modules"]

versioning:
  # 语义相似度阈值 (超过此值触发仲裁)
  semantic_similarity_threshold: 0.85
  # [极客模式] 是否物理删除旧版本 (默认: false, 推荐: true)
  delete_superseded: true 
```

## 🔌 客户端接入

### Claude Desktop / Code

编辑 `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) 或相应位置：

```json
{
  "mcpServers": {
    "agent-mem": {
      "command": "/absolute/path/to/agent-mem",
      "args": ["--transport", "stdio", "--watch"]
    }
  }
}
```

### Cursor (Beta)

在 Cursor 的 MCP 设置中添加：
*   **Type**: SSE
*   **URL**: `http://127.0.0.1:8787/sse`

## 🛠️ 开发指南

项目结构：
*   `mcp-go/`: 核心源码
    *   `cmd/`: 入口文件
    *   `ingest.go`: 入库流水线
    *   `watcher.go`: 文件监控
    *   `llm.go`: Prompt 工程
*   `scripts/`: 测试脚本 (如 `e2e_test_go.py`)

运行 E2E 测试：
```bash
# 需要 Python 环境
python scripts/e2e_test_go.py
```

## License

MIT
