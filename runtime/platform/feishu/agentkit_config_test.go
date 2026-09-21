package feishu

import (
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestNewPlatformDefaultShowToolProgress(t *testing.T) {
	ws, err := rw.New(rw.Config{Global: t.TempDir(), Local: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	plat, err := newPlatform("feishu", lark.FeishuBaseUrl, Config{
		AppID:     "app",
		AppSecret: "secret",
	}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := plat.(*Platform)
	if !ok {
		t.Fatalf("got %T", plat)
	}
	if !p.showToolProgress {
		t.Fatal("showToolProgress should default true")
	}
}
