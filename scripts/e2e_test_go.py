import os
import time
import shutil
import subprocess
import signal
from pathlib import Path
from sqlalchemy import delete, select
from src.db import get_db, KnowledgeBlock
from src.core.searcher import Searcher

# 定义测试项目配置
PROJECT_ID = "e2e_test_go_watcher"
MACHINE_ID = "test-host-go"
ROOT_DIR = Path("tmp/e2e_test_env_go")

# 模拟的测试文档内容
MOCK_DOCS = {
    "docs/architecture/system_design.md": "# System Architecture (Go Version)\n\n## Overview\nThe system has been migrated to pure Go.\n1. **Watcher**: Uses fsnotify.\n2. **Ingester**: Pure Go implementation of the pipeline.\n",
    "insights/go_migration_lessons.md": "---\nknowledge_type: insight\ninsight_type: lesson\ntags: [golang, migration]\n---\n# Go Migration Lessons\n\n- **Problem**: Python dependency management is slow.\n- **Solution**: Rewrite in Go.\n- **Result**: Single binary, fast startup.\n"
}

def setup_env():
    """准备测试环境"""
    print(f"🛠️  Preparing environment: {ROOT_DIR}")
    if ROOT_DIR.exists():
        shutil.rmtree(ROOT_DIR)
    ROOT_DIR.mkdir(parents=True)
    
    # 预先创建所有目录
    print("📁 Creating directories...")
    for rel_path in MOCK_DOCS.keys():
        path = ROOT_DIR / rel_path
        path.parent.mkdir(parents=True, exist_ok=True)
        print(f"   Created: {path.parent}")

    # 创建项目标识
    (ROOT_DIR / ".project.yaml").write_text(f"project_id: {PROJECT_ID}\nproject_name: Go E2E测试", encoding="utf-8")
    
    # 清理数据库
    print("🧹 Cleaning DB...")
    db = next(get_db())
    try:
        db.execute(delete(KnowledgeBlock).where(KnowledgeBlock.project_id == PROJECT_ID))
        db.commit()
    finally:
        db.close()

def start_go_watcher():
    """启动 Go Watcher"""
    print("\n🚀 Starting Go Watcher...")
    
    # 构造配置文件
    config_path = ROOT_DIR / "settings.yaml"
    config_content = f"""
project:
  default_project_id: {PROJECT_ID}
watcher:
  roots: ["{ROOT_DIR.absolute()}"]
  watch_dirs: ["docs", "insights"]
  extensions: [".md"]
  debounce_seconds: 1
storage:
  database_url: postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge
llm:
  api_key_env: DASHSCOPE_API_KEY
  model_distill: qwen-plus
  model_summary: qwen-turbo
  model_relation: qwen-turbo
  model_arbitrate: qwen-flash
embedding:
  provider: qwen
  model: text-embedding-v4
"""
    config_path.write_text(config_content, encoding="utf-8")

    # 启动进程
    cmd = [
        "./out/agent-mem-mcp",
        "--watch",
        "--config", str(config_path.absolute())
    ]
    
    # 设置环境变量
    env = os.environ.copy()
    env["HOST_ID"] = MACHINE_ID
    
    process = subprocess.Popen(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
        preexec_fn=os.setsid
    )
    
    # 等待启动
    print("   ... Waiting for watcher to start (3s) ...")
    time.sleep(3)
    if process.poll() is not None:
        stdout, stderr = process.communicate()
        print(f"❌ Watcher failed to start:\nSTDOUT: {stdout}\nSTDERR: {stderr}")
        return None
        
    print("✅ Watcher started")
    return process

def write_files():
    """写入文件触发 Watcher"""
    print("\n📝 Writing test docs...")
    for rel_path, content in MOCK_DOCS.items():
        file_path = ROOT_DIR / rel_path
        print(f"   + Writing {rel_path}")
        try:
            file_path.write_text(content.strip(), encoding="utf-8")
        except Exception as e:
            print(f"❌ Write failed: {e}")
        time.sleep(1)

def run_db_verification():
    """验证数据库"""
    print("\n📊 Verifying DB...")
    
    max_retries = 10
    db = next(get_db())
    
    try:
        for i in range(max_retries):
            time.sleep(2)
            count = db.execute(
                select(KnowledgeBlock).where(KnowledgeBlock.project_id == PROJECT_ID)
            ).scalars().all()
            print(f"   [{i+1}/{max_retries}] Records: {len(count)} (Expected: {len(MOCK_DOCS)})")
            
            if len(count) >= len(MOCK_DOCS):
                print("✅ Success: All records found")
                
                # 验证 Insight
                insights = db.execute(
                    select(KnowledgeBlock).where(
                        KnowledgeBlock.project_id == PROJECT_ID,
                        KnowledgeBlock.knowledge_type == 'insight'
                    )
                ).scalars().all()
                print(f"   Insights found: {len(insights)}")
                if len(insights) > 0:
                    print(f"   - Insight Title: {insights[0].title}")
                return True
        
        print("❌ Timeout: Missing records")
        return False
    finally:
        db.close()

def stop_watcher(process):
    if process:
        print("\n🛑 Stopping Watcher...")
        os.killpg(os.getpgid(process.pid), signal.SIGTERM)
        process.wait()
        # 打印日志
        stdout, stderr = process.communicate()
        print("--- Watcher Logs ---")
        print(stdout)
        print(stderr)
        print("--------------------")

if __name__ == "__main__":
    setup_env()
    watcher_proc = start_go_watcher()
    if watcher_proc:
        try:
            write_files()
            success = run_db_verification()
            if not success:
                exit(1)
        finally:
            stop_watcher(watcher_proc)
