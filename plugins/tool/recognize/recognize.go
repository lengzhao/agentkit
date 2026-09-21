package recognize

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	rtllm "github.com/lengzhao/agentkit/runtime/llm"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

const (
	defaultImageSystemPrompt = "You are a vision assistant. Describe the image accurately for another agent. " +
		"Start with a one-sentence conclusion, then list key visible details (text, UI, numbers). " +
		"Say when something is unreadable; do not guess."
)

type RecognizeConfig struct {
	// Model overrides the vision LLM model; empty uses the provider default.
	Model string `json:"model"`
	// ImageSystemPrompt overrides the default vision system instruction.
	ImageSystemPrompt string `json:"imageSystemPrompt"`
}

type RecognizeDeps struct {
	LLM       agentkit.LLMProvider   `json:"llm"`
	Workspace workspace.Service      `json:"workspace"`
	Telemetry captelemetry.Toolkit   `json:"telemetry"`
}

type RecognizeImageInput struct {
	Path string `json:"path" jsonschema:"Workspace image path (e.g. upload/photo.png)"`
	Task string `json:"task,omitempty" jsonschema:"Optional focus or question about the image"`
}

// NewRecognize registers tool/recognize: recognize_image via deps.llm and config.model.
func NewRecognize(cfg RecognizeConfig, deps RecognizeDeps) (agentkit.ToolPack, error) {
	if deps.LLM == nil {
		return nil, fmt.Errorf("tool/recognize requires llm dependency")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/recognize requires workspace dependency")
	}
	if deps.Telemetry == nil {
		return nil, fmt.Errorf("tool/recognize requires telemetry dependency")
	}
	sysPrompt := strings.TrimSpace(cfg.ImageSystemPrompt)
	if sysPrompt == "" {
		sysPrompt = defaultImageSystemPrompt
	}
	svc := &service{
		model:     strings.TrimSpace(cfg.Model),
		llm:       deps.LLM,
		ws:        deps.Workspace,
		telemetry: deps.Telemetry,
		sysPrompt: sysPrompt,
	}

	imageTool, err := agentkit.NewTool[RecognizeImageInput, string]("recognize_image", svc.recognizeImage).
		Description("Run vision on a workspace image and return a text description (OCR, UI, charts).").
		Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(imageTool), nil
}

type service struct {
	model     string
	llm       agentkit.LLMProvider
	ws        workspace.Service
	telemetry captelemetry.Toolkit
	sysPrompt string
}

func (s *service) recognizeImage(ctx context.Context, input RecognizeImageInput) (string, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	data, mime, err := rtmedia.LoadWorkspaceImage(ctx, s.ws, path, 0)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("not an image or file too large: %s", path)
	}
	userText := strings.TrimSpace(input.Task)
	if userText == "" {
		userText = "Describe this image."
	}
	slog.Info("recognize_image", "path", path, "mime", mime, "bytes", len(data))
	s.telemetry.RecordEvent(ctx, "vision.hydrate", map[string]string{
		"path":      path,
		"mime":      mime,
		"out_bytes": fmt.Sprint(len(data)),
		"source":    "recognize_tool",
	})
	return rtllm.CompleteText(ctx, s.llm, agentkit.LLMRequest{
		Model: s.model,
		Messages: []agentkit.ModelMessage{
			{Role: "system", Content: []agentkit.ContentPart{{Type: "text", Text: s.sysPrompt}}},
			{Role: "user", Content: []agentkit.ContentPart{
				{Type: "text", Text: userText},
				{Type: "image_url", URL: rtmedia.DataURL(mime, data), MIME: mime},
			}},
		},
	})
}
