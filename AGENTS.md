# Repository Guidelines

## 项目结构与模块组织
源码 在 `src/`，其中 `src/core/` 包含 入库、向量化、检索、LLM 适配 等 核心流程；`src/server.py` 提供 FastAPI 接口，`src/watcher.py` 负责 文件监控。测试 在 `tests/`，脚本 在 `scripts/`，配置 在 `config/settings.yaml`，文档 在 `docs/`。对话 记录 通常 放在 `chat_history/` 并 会 被 提炼 入库。

## 架构概览
系统 采用 LLM 提炼 + 关系 抽取 + 向量 检索 的 链路，入库 流程 为 分类 → 提炼 → 关系 → 向量 → 语义 替换 → 保存。检索 由 意图 路由 决定 范围 与 排序。

## 构建、测试与本地开发命令
- `docker-compose up -d`：启动 PostgreSQL + pgvector。
- `python -m venv .venv && source .venv/bin/activate && pip install -r requirements.txt`：创建 虚拟环境 并 安装 依赖。
- `PYTHONPATH=$(pwd) python src/server.py`：启动 API 服务。
- `PYTHONUNBUFFERED=1 PYTHONPATH=$(pwd) python src/watcher.py`：启动 监控 入库。
- `PYTHONPATH=. .venv/bin/python scripts/init_db.py`：初始化 数据库 与 vector 扩展。

## 编码风格与命名约定
Python 统一 4 空格 缩进，函数 使用 `snake_case`，类 使用 `CamelCase`，常量 使用 全大写。保持 模块 边界 清晰，核心逻辑 放在 `src/core/`，避免 在 入口 文件 堆积 业务逻辑。未集成 自动格式化 时 请 遵循 PEP 8 并 控制 行宽。

## 测试规范
测试 使用 `unittest`，文件 命名 `tests/test_*.py`。运行 命令：`PYTHONPATH=. .venv/bin/python -m unittest discover -s tests`。测试 配置 参考 `tests/config_test.yaml`，默认 使用 mock 向量，避免 真实 API 依赖。

## 提交与 PR 规范
提交 信息 采用 约定式 前缀，历史 主要 使用 `feat:`、`docs:`、`chore:`。提交 描述 要 说明 变更 范围 与 影响。PR 需 包含 变更 摘要、测试 命令 与 结果，如涉及 embedding 维度 或 Schema 变化，请 附 数据库 迁移/重建 说明。

## 配置与安全提示
密钥 放在 `.env` 或 `~/.config/agent_tools.env`，不要 提交 到 仓库。关键 变量 包含 `DASHSCOPE_API_KEY`、`DASHSCOPE_BASE_URL`、`DATABASE_URL`。修改 `embedding.model` 或 `embedding.dimension` 前，先 评估 数据库 向量 维度 兼容性。
