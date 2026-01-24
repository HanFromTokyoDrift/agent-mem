# Python to Go 迁移报告

> **日期**: 2026-01-24
> **状态**: ✅ 完成
> **验证**: E2E 测试通过

## 1. 迁移概述

本项目 `agent-mem` 的核心组件已从混合架构（Python 写入 / Go 读取）成功迁移至 **纯 Go 架构**。这极大地简化了部署流程，提升了运行时性能，并消除了对 Python 环境的依赖。

## 2. 完成工作

### 2.1 核心组件重构
- **Watcher (Go)**:
  - 引入 `fsnotify/fsnotify` 库。
  - 实现了递归目录监听、文件变更捕获（Create/Write）。
  - 实现了防抖（Debounce）逻辑，避免重复触发。
  - 支持 `.gitignore` 风格的排除规则。
- **Ingester (Go)**:
  - 完整复刻了 Python 版 `ingester.py` 的流水线：
    - `ProcessFile`: Frontmatter 解析、哈希计算、文档类型推断。
    - `Distill`: 调用 Qwen API 提炼对话洞见。
    - `Summarize`: 自动生成长文摘要。
    - `ResolveRelations`: 提取文档引用关系。
    - `Embed`: 向量化（支持 Mock 和 Qwen）。
    - `SemanticReplace`: 向量检索相似度 + LLM 仲裁，实现智能版本管理。
- **Database (Go)**:
  - 扩展了 `db.go`，增加了 `SaveKnowledgeBlock` 和 `DeprecateBlock` 等写入操作。
  - 实现了完整的事务管理。

### 2.2 遗留代码归档
- 原 `src/` 目录已移动至 `src_legacy/`。
- `README.md` 已更新，移除了 Python 相关的启动说明。

## 3. 验证结果

执行了端到端 (E2E) 测试脚本 `scripts/e2e_test_go.py`，验证流程如下：
1. **环境准备**: 清理数据库，预创建目录结构。
2. **服务启动**: 启动编译后的 `./out/agent-mem-mcp --watch`。
3. **文件变更**: 模拟写入 `system_design.md` (Doc) 和 `migration_lessons.md` (Insight)。
4. **自动入库**: Watcher 捕获事件 -> Ingester 处理 -> DB 存储。
5. **结果校验**:
   - 数据库中成功查询到 2 条记录。
   - `migration_lessons.md` 正确被识别为 `Insight` 类型。
   - 向量检索和 Rerank 功能正常。

## 4. 后续建议

- **回归测试**: 建议将 `scripts/e2e_test_go.py` 加入 CI/CD 流程。
- **多机部署**: 目前 Watcher 依赖本地文件系统事件，多机部署时需考虑文件同步或分布式存储方案。
- **Prompt 优化**: Go 代码中的 Prompt 字符串目前是硬编码的，后续可考虑提取到外部配置文件或资源文件中。

## 5. 如何运行新版

```bash
# 编译
cd mcp-go && go build -o ../out/agent-mem-mcp ./cmd/agent-mem-mcp

# 运行 (Watcher + Server)
../out/agent-mem-mcp --watch --transport http
```
