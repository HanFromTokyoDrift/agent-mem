# Project Cortex 架构设计

> **AI Agent 认知资产管理系统**
> 自动入库、语义检索、版本演进、对话炼金

## 1. 项目定位

Project Cortex（agent-mem）是一个本地优先的知识管理系统，解决「上下文遗忘」问题：
- 文档与对话自动入库
- 语义搜索与意图路由
- 新旧版本替换与历史保留
- 语义连接图（关联需求/设计/实现/复盘）

## 2. 技术栈

- **数据库**：PostgreSQL 16 + pgvector
- **后端**：FastAPI + SQLAlchemy 2.0
- **向量化**：千问 Embedding（OpenAI 兼容接口）
- **LLM**：Qwen 全家桶（提炼 / 仲裁 / 路由 / 关系抽取）
- **监控**：watchdog（文件系统事件）

## 3. 数据流（完整版）

1. **Watcher**：检测文档/对话变化
2. **Classifier**：推断 doc_type / knowledge_type
3. **Distill**：对话提炼为结构化干货
4. **Extract Relations**：抽取引用关系并检索匹配
5. **Embed**：生成向量
6. **Semantic Replace**：语义替换仲裁
7. **Save**：写入数据库

## 4. 分类体系

**doc_type（文档类型）**
- background / requirements / architecture / design / implementation / progress / testing / deployment / delivery

**knowledge_type（知识类型）**
- doc / insight / dialogue_extract

**insight_type（洞见类型）**
- solution / lesson / pattern / decision

## 5. 语义连接图

通过 `related_ids` 建立弱连接：
- based_on / references / implements / validates / supersedes

例：
- 架构文档基于需求文档（based_on）
- 复盘文档引用故障报告（references）

## 6. 意图路由

系统先判断用户意图，再选择检索策略：
- 进度问题 → progress / issue，限制最近 3 天
- 决策问题 → architecture / insight / background
- 部署问题 → deployment / delivery（必须最新）

## 7. 关键文件

- `src/watcher.py`：文件监控
- `src/ingester.py`：文件分类与元数据抽取
- `src/core/ingester.py`：核心入库流程
- `src/core/llm.py`：千问处理（提炼/仲裁/路由/关系抽取）
- `src/core/searcher.py`：向量检索
- `src/db.py`：数据库模型
