# 本地 AI 项目数据库服务

> 统一的 PostgreSQL + pgvector 服务，供所有 AI 项目使用

## 服务信息

| 项目 | 值 |
|:---|:---|
| **容器名** | `agent-mem-db` |
| **镜像** | `pgvector/pgvector:pg16` |
| **主机端口** | `5440` |
| **重启策略** | `always`（开机自启） |

## 连接信息

```bash
# 通用连接字符串模板
postgresql://<user>:<password>@localhost:5440/<database>

# 超级用户（可创建数据库）
Host:     localhost
Port:     5440
User:     cortex
Password: cortex_password_secure
Database: cortex_knowledge (默认)
```

## 已创建的数据库

| 数据库名 | 用途 | 项目 |
|:---|:---|:---|
| `cortex_knowledge` | 认知资产管理 | Project Cortex |
| `postgres` | 系统默认 | - |

## 使用方法

### 1. 创建新数据库（为新项目）

```bash
# 方法 A: 命令行
docker exec agent-mem-db psql -U cortex -c "CREATE DATABASE my_new_project;"

# 方法 B: 通过 psql 连接
psql -h localhost -p 5440 -U cortex -c "CREATE DATABASE my_new_project;"
```

### 2. 连接数据库

```bash
# 命令行
psql -h localhost -p 5440 -U cortex -d cortex_knowledge

# Python (psycopg)
DATABASE_URL = "postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge"

# Docker 内部连接（同一网络）
DATABASE_URL = "postgresql://cortex:cortex_password_secure@agent-mem-db:5432/cortex_knowledge"
```

### 3. 启用 pgvector 扩展

新数据库需要手动启用 pgvector：

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

## 管理命令

```bash
# 查看容器状态
docker ps -f name=agent-mem-db

# 查看日志
docker logs agent-mem-db

# 进入 psql
docker exec -it agent-mem-db psql -U cortex

# 备份数据库
docker exec agent-mem-db pg_dump -U cortex cortex_knowledge > backup.sql

# 恢复数据库
cat backup.sql | docker exec -i agent-mem-db psql -U cortex cortex_knowledge
```

## 各项目连接配置示例

### Project Cortex

```python
# src/db.py
DB_URL = "postgresql+psycopg://cortex:cortex_password_secure@localhost:5440/cortex_knowledge"
```

### 其他 AI 项目

```python
# 1. 先创建数据库
# docker exec agent-mem-db psql -U cortex -c "CREATE DATABASE project_alpha;"
# docker exec agent-mem-db psql -U cortex -d project_alpha -c "CREATE EXTENSION vector;"

# 2. 使用连接
DB_URL = "postgresql+psycopg://cortex:cortex_password_secure@localhost:5440/project_alpha"
```

## Claude Code MCP 配置

已配置 PostgreSQL MCP，Claude Code 可以直接操作数据库。

**配置文件**：`~/.claude/mcp.json`

```json
{
  "postgres-cortex": {
    "command": "npx",
    "args": [
      "-y",
      "@modelcontextprotocol/server-postgres",
      "postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge"
    ],
    "env": {}
  }
}
```

**使用方法**：重启 Claude Code 后，可以直接让 AI 执行 SQL 查询。

## 注意事项

1. **端口 5440**：避免与其他 PG 实例冲突
2. **数据持久化**：数据存储在 Docker volume `cortex_data`
3. **pgvector**：每个新数据库需要单独启用扩展
4. **备份**：定期备份重要数据
5. **MCP 生效**：修改 mcp.json 后需要重启 Claude Code
