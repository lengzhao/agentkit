package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// Use one file per user and instance so separate users cannot overwrite each
// other's preferences. Hash identifiers rather than accepting them as paths.
func (p *Platform) dmPreferencePath(ctx context.Context, userID string) (string, error) {
	if p.workspace == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	identity, _ := json.Marshal([]string{p.tag(), p.cfg.AppID, p.PlatformInstance(), userID})
	return p.workspace.Resolve(ctx, fmt.Sprintf("preferences/dm-thread/%x.json", sha256.Sum256(identity)))
}

func (p *Platform) dmThreadEnabled(userID string) bool {
	p.dmPreferenceMu.Lock()
	defer p.dmPreferenceMu.Unlock()
	path, err := p.dmPreferencePath(context.Background(), userID)
	if err != nil {
		return p.cfg.DMThreadMode
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return p.cfg.DMThreadMode
	}
	var enabled bool
	if err == nil {
		err = json.Unmarshal(data, &enabled)
	}
	if err != nil {
		slog.Warn(p.tag()+": cannot read DM thread preference", "error", err)
		return p.cfg.DMThreadMode
	}
	return enabled
}

func (p *Platform) setDMThreadMode(ctx context.Context, userID string, enabled bool) error {
	p.dmPreferenceMu.Lock()
	defer p.dmPreferenceMu.Unlock()
	path, err := p.dmPreferencePath(ctx, userID)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".dm-thread-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	data, _ := json.Marshal(enabled)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (p *Platform) dmThreadCommand(ctx context.Context, userID, text string) (bool, string) {
	name, args, ok := common.ParseSlashCommand(text)
	if !ok || name != "dm-thread" {
		return false, ""
	}
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "on", "off":
		enabled := strings.EqualFold(strings.TrimSpace(args), "on")
		if err := p.setDMThreadMode(ctx, userID, enabled); err != nil {
			return true, fmt.Sprintf("保存 DM 话题设置失败: %v", err)
		}
	case "", "status":
	default:
		return true, "用法：/dm-thread on|off|status"
	}
	if p.dmThreadEnabled(userID) {
		return true, "DM 话题模式已开启：新消息创建独立话题，在话题内回复可继续对话。独立上下文需要管理员配置 runner.sessionScope: thread。"
	}
	return true, "DM 话题模式已关闭：新消息使用平铺对话，已有话题内仍可继续回复。"
}
