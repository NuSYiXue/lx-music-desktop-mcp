// lx-music-mcp 是 LX Music Desktop 的 MCP server。
//
// 它以 stdio 方式与 host（Reasonix / Claude Desktop / Cursor / Cline 等）通信，
// 本身是无状态的 HTTP 客户端：所有实际操作都通过 LX 的 Open API 完成。
//
// 环境变量：
//
//	LX_OPEN_API   LX Open API 的完整地址，默认 http://127.0.0.1:23330
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/musicstore"
	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/tools"
)

// version 由构建脚本通过 -ldflags "-X main.version=..." 注入。
var version = "dev"

// EnvOpenAPI 是覆盖 Open API 地址的环境变量名。
const EnvOpenAPI = "LX_OPEN_API"

func main() {
	// stdout 专供 JSON-RPC。任何东西写到 stdout 都会污染协议流、
	// 让 host 直接解析失败，所以日志一律走 stderr。
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	base := os.Getenv(EnvOpenAPI)

	server := mcp.NewServer(
		&mcp.Implementation{Name: "lx-music", Version: version},
		&mcp.ServerOptions{Instructions: tools.Instructions},
	)

	tools.Register(server, &tools.Deps{
		API:    lxapi.New(base),
		Store:  musicstore.New(musicstore.DefaultLimit),
		Volume: tools.NewVolumeTracker(),
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "lx-music-mcp 退出：", err)
		os.Exit(1)
	}
}
