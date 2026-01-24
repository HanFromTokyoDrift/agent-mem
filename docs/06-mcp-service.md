# MCP 服务（Agent Memory）

本项目提供 MCP 服务端，统一给 Claude / Gemini / Codex 等客户端使用，避免每个 Agent 单独适配。

## 启动方式

### Go MCP 服务（推荐）

```bash
cd mcp-go
go build -o ../out/agent-mem-mcp ./cmd/agent-mem-mcp
../out/agent-mem-mcp --host 127.0.0.1 --port 8787 --transport http
```

说明：
- `http` 同时开启 `/sse` 与 `/mcp`，建议默认使用
- `stdio` 适合本地调试：`../out/agent-mem-mcp --transport stdio`
- Go 版直接连接数据库与 LLM/向量服务，不依赖 Python API

### Python MCP（可选）

如果仍需使用 Python 版：

```bash
PYTHONPATH=$(pwd) .venv/bin/python src/mcp_server.py --transport sse --host 127.0.0.1 --port 8787
```

### HTTP SSE（Python）

```bash
PYTHONPATH=$(pwd) .venv/bin/python src/mcp_server.py --transport sse --host 127.0.0.1 --port 8787
```

### Streamable HTTP（Python，可选）

```bash
PYTHONPATH=$(pwd) .venv/bin/python src/mcp_server.py --transport streamable-http --host 127.0.0.1 --port 8787
```

说明：SSE 更通用，适合多客户端；stdio 更适合单机调试，但需要额外处理进程生命周期。

## 工具说明

### mem.write_memory

写入 Markdown 并触发入库。

参数要点：
- `project_root`：项目根目录（必填）
- `relative_path`：可选，未传时自动生成
- `knowledge_type / insight_type / tags`：可选，自动写入 YAML Front Matter

### mem.search

语义检索（默认带意图路由），返回精简索引信息。
可选参数：`use_rerank`（启用重排序时会调用 `gte-rerank-v2`）。

### mem.get_observations

按 ID 批量拉取完整内容。

### mem.timeline

以 anchor 或 query 为中心，拉取时间窗口内的上下文列表。

## 自动路径规则

当 `relative_path` 为空时，系统按以下规则生成：
- `knowledge_type=dialogue_extract` → `chat_history/`
- `insight_type=lesson` → `lessons/`
- `insight_type=solution/pattern/decision` 或 `knowledge_type=insight` → `insights/`
- 其他 → `notes/`

文件名格式：`YYYY-MM-DD_HH-MM-SS_<slug>.md`，slug 默认取标题（纯 ASCII）。

## 推荐记忆内容结构（Markdown）

```markdown
---
knowledge_type: insight
insight_type: decision
tags:
  - mcp
  - memory
---
# 方案选择：Embedding 维度
- 结论：text-embedding-v4，1024 维
- 原因：统一维度，兼容现有向量库
- 风险：Rerank 权限不足时自动降级
```

## 客户端配置示例

### Claude Code（`~/.claude/mcp.json`）

```json
{
  "mcpServers": {
    "agent-mem": {
      "url": "http://127.0.0.1:8787/sse"
    }
  }
}
```

### Codex CLI（`~/.codex/config.toml`）

```toml
[mcp_servers.agent-mem]
url = "http://127.0.0.1:8787/sse"
```

### Gemini CLI（`~/.gemini/config.yaml`）

```yaml
mcpServers:
  agent-mem:
    url: http://127.0.0.1:8787/sse
```

如需使用 `streamable-http`，将 `url` 改为 `http://127.0.0.1:8787/mcp`。

## 依赖说明

Go 版依赖独立管理在 `mcp-go/`，避免影响 Python 环境。需要确保：
1) PostgreSQL（`docker-compose up -d`）  
2) 已配置 `DASHSCOPE_API_KEY` 与 `DATABASE_URL`（可放在 `~/.config/agent_tools.env`）  
如需同时提供 HTTP API，再单独启动 `src/server.py`。
