package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	var (
		host      = flag.String("host", defaultHost, "监听地址")
		port      = flag.Int("port", defaultPort, "监听端口")
		transport = flag.String("transport", "http", "传输方式：http/sse/streamable/stdio")
		config    = flag.String("config", "", "配置文件路径")
		watchMode = flag.Bool("watch", false, "启动文件监控模式")
	)
	flag.Parse()

	settings, err := loadSettings(*config)
	if err != nil {
		panic(err)
	}

	app, err := NewApp(settings)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	if err := app.EnsureSchema(context.Background()); err != nil {
		panic(err)
	}

	if *watchMode {
		fmt.Printf("🚀 启动 Watcher 模式\n")
		watcher, err := NewWatcher(app)
		if err != nil {
			panic(err)
		}
		defer watcher.Close()

		roots := settings.Watcher.Roots
		roots = append(roots, settings.Watcher.ExtraRoots...)
		if len(roots) == 0 {
			roots = []string{"."}
		}

		watcher.Start(roots)

		// 阻塞
		select {}
	}

	server := buildServer(app)

	switch strings.ToLower(*transport) {
	case "stdio":
		ctx := context.Background()
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			panic(err)
		}
		return
	case "sse", "streamable", "http", "both":
		// 继续 HTTP 模式
	default:
		panic(fmt.Errorf("不支持的 transport: %s", *transport))
	}

	mux := http.NewServeMux()
	if *transport == "sse" || *transport == "http" || *transport == "both" {
		sseHandler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
		mux.Handle("/sse", sseHandler)
	}
	if *transport == "streamable" || *transport == "http" || *transport == "both" {
		streamHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
		mux.Handle("/mcp", streamHandler)
	}

	addr := fmt.Sprintf("%s:%d", *host, *port)
	fmt.Printf("MCP 服务启动: http://%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(err)
	}
}