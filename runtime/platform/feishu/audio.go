package feishu

import (
	"context"
	"fmt"
)

func convertAudioToOpus(ctx context.Context, audio []byte, srcFormat string) ([]byte, error) {
	_ = ctx
	if srcFormat == "opus" {
		return audio, nil
	}
	return nil, fmt.Errorf("%s: audio conversion to opus requires ffmpeg (format %q)", "feishu", srcFormat)
}
