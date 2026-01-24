package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	app      *App
	fsNotify *fsnotify.Watcher
	debounce map[string]time.Time
	mu       sync.Mutex
	done     chan struct{}
}

func NewWatcher(app *App) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		app:      app,
		fsNotify: fsWatcher,
		debounce: make(map[string]time.Time),
		done:     make(chan struct{}),
	}, nil
}

func (w *Watcher) Close() {
	if w.fsNotify != nil {
		w.fsNotify.Close()
	}
	close(w.done)
}

func (w *Watcher) Start(roots []string) {
	if len(roots) == 0 {
		cwd, err := os.Getwd()
		if err == nil {
			roots = []string{cwd}
			log.Printf("⚠️ 未配置监控目录，默认监听当前目录: %s", cwd)
		}
	}

	for _, root := range roots {
		if root == "" || !exists(root) {
			continue
		}
		w.addRecursive(root)
	}

	go w.eventLoop()
}

func (w *Watcher) addRecursive(root string) {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if w.shouldIgnoreDir(path) {
				return filepath.SkipDir
			}
			if err := w.fsNotify.Add(path); err != nil {
				log.Printf("❌ 无法监听目录 %s: %v", path, err)
			} else {
				log.Printf("👀 监听目录: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("❌ 遍历目录失败: %v", err)
	}
}

func (w *Watcher) eventLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.fsNotify.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.fsNotify.Errors:
			if !ok {
				return
			}
			log.Printf("❌ Watcher 错误: %v", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// DEBUG LOG
	log.Printf("EVENT: %s | Op: %v", event.Name, event.Op)

	// 忽略删除和重命名
	if event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename {
		return
	}

	// 新建目录
	if event.Op&fsnotify.Create == fsnotify.Create {
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			if !w.shouldIgnoreDir(event.Name) {
				w.fsNotify.Add(event.Name)
				w.addRecursive(event.Name)
			}
			return
		}
	}

	if event.Op&fsnotify.Create != fsnotify.Create && event.Op&fsnotify.Write != fsnotify.Write {
		return
	}

	path := event.Name
	if w.shouldIgnoreFile(path) {
		log.Printf("Ignoring file: %s", path)
		return
	}

	// 防抖
	w.mu.Lock()
	lastTime, ok := w.debounce[path]
	now := time.Now()
	// debounce 1s
	if ok && now.Sub(lastTime) < 1*time.Second {
		log.Printf("Debounced: %s", path)
		w.mu.Unlock()
		return
	}
	w.debounce[path] = now
	w.mu.Unlock()

	log.Printf("⚡ 准备入库: %s", path)

	go func(p string) {
		time.Sleep(100 * time.Millisecond)
		machineID := envOrDefault("HOST_ID", "mcp-go-watcher")
		res, err := ingestFile(context.Background(), w.app, p, "", machineID)
		if err != nil {
			log.Printf("❌ 入库失败 [%s]: %v", p, err)
		} else if res.Status != "skipped" {
			log.Printf("✅ 入库成功 [%s]: ID=%s", p, res.ID)
		} else {
			log.Printf("⏩ 跳过文件 [%s]: %s", p, res.Reason)
		}
	}(path)
}

func (w *Watcher) shouldIgnoreDir(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") && base != "." {
		return true
	}
	for _, ignore := range w.app.settings.Watcher.IgnoreDirs {
		if base == ignore {
			return true
		}
	}
	return false
}

func (w *Watcher) shouldIgnoreFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return true
	}
	// 简单匹配后缀
	ext := filepath.Ext(path)
	allowed := false
	for _, e := range w.app.settings.Watcher.Extensions {
		if e == ext {
			allowed = true
			break
		}
	}
	if !allowed {
		// log.Printf("Ignore ext: %s (allowed: %v)", ext, w.app.settings.Watcher.Extensions)
		return true
	}
	return false
}