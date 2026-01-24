# 技术选型

## 总体架构
PostgreSQL + pgvector 是核心存储。
千问 Embedding 负责向量化。
千问（Flash / Turbo / Plus）负责逻辑仲裁与提炼，优先复用 `agent_tools/llm_sdk.py`。

## 关键决策
- **不引入 Qdrant/Milvus**：保持单体架构，减少运维复杂度。
- **统一千问链路**：模型一致、接口统一，自动从 `~/.config/agent_tools.env` 读取 Key。
