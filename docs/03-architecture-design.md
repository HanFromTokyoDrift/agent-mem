# 架构设计

## 模块划分
- **Watcher**: 监控文件系统，实现防抖。
- **Ingester**: 核心流水线控制器。
- **Distiller**: 对话提炼器 (LLM)。
- **Arbiter**: 新旧版本仲裁器 (LLM)。

## 数据流
File -> Watcher -> Classifier -> (Distiller) -> Embedder -> Arbiter -> DB