package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/musicstore"
)

// TestRegisterBuildsValidSchemas 守住一个真实踩过的坑：
//
// jsonschema 标签的描述里如果出现英文逗号或等号，google/jsonschema-go
// 会把它当成「选项=值」解析，AddTool 直接 panic。而 go build 和 go vet
// 都发现不了——只有真正跑起来才会崩。
//
// 这个测试让这类错误在单测阶段就暴露，而不是等到用户接进 host 才发现。
func TestRegisterBuildsValidSchemas(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "lx-music", Version: "test"}, nil)
	Register(server, &Deps{
		API:   lxapi.New("http://127.0.0.1:1"),
		Store: musicstore.New(10),
	})
}
