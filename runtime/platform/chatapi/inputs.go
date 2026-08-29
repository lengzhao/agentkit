package chatapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type chatInput struct {
	Type           string `json:"type"`
	TransferMethod string `json:"transfer_method"`
	Data           string `json:"data"`
	UploadFileID   string `json:"upload_file_id"`
	MimeType       string `json:"mime_type"`
	Filename       string `json:"filename"`
}

func (p *Platform) inputsToCore(ctx context.Context, channelKey string, inputs []chatInput) (
	images []common.ImageAttachment,
	files []common.FileAttachment,
	audio *common.AudioAttachment,
	filePaths []string,
	err error,
) {
	for _, in := range inputs {
		method := strings.ToLower(strings.TrimSpace(in.TransferMethod))
		if method == "" {
			method = "base64"
		}
		var data []byte
		var workRel string
		mimeType := in.MimeType
		filename := in.Filename
		switch method {
		case "base64":
			data, err = base64.StdEncoding.DecodeString(in.Data)
			if err != nil {
				return nil, nil, nil, nil, err
			}
		case "local_file":
			workRel, mimeType, filename, err = p.resolveUploadedInput(ctx, channelKey, in)
			if err != nil {
				if errors.Is(err, errUploadNotFound) {
					return nil, nil, nil, nil, fmt.Errorf("upload not found")
				}
				return nil, nil, nil, nil, err
			}
		default:
			return nil, nil, nil, nil, fmt.Errorf("unsupported transfer_method")
		}
		switch strings.ToLower(in.Type) {
		case "image":
			if len(data) == 0 && workRel != "" {
				abs, err := p.workspace.Resolve(p.channelCtx(ctx, channelKey), "work/"+workRel)
				if err != nil {
					return nil, nil, nil, nil, err
				}
				data, err = os.ReadFile(abs)
				if err != nil {
					return nil, nil, nil, nil, err
				}
			}
			images = append(images, common.ImageAttachment{
				MimeType: mimeType, Data: data, FileName: filename,
			})
		case "file":
			if workRel != "" {
				filePaths = append(filePaths, workRel)
				continue
			}
			files = append(files, common.FileAttachment{
				MimeType: mimeType, Data: data, FileName: filename,
			})
		case "audio":
			if audio != nil {
				return nil, nil, nil, nil, fmt.Errorf("only one audio input supported")
			}
			if len(data) == 0 && workRel != "" {
				abs, err := p.workspace.Resolve(p.channelCtx(ctx, channelKey), "work/"+workRel)
				if err != nil {
					return nil, nil, nil, nil, err
				}
				data, err = os.ReadFile(abs)
				if err != nil {
					return nil, nil, nil, nil, err
				}
			}
			audio = &common.AudioAttachment{MimeType: mimeType, Data: data}
		default:
			return nil, nil, nil, nil, fmt.Errorf("unsupported input type")
		}
	}
	return images, files, audio, filePaths, nil
}

func (p *Platform) resolveUploadedInput(ctx context.Context, channelKey string, in chatInput) (workRel, mimeType, filename string, err error) {
	fileID := strings.TrimSpace(in.UploadFileID)
	if fileID == "" {
		return "", "", "", fmt.Errorf("upload_file_id required")
	}
	workRel, err = p.uploadedWorkPath(ctx, channelKey, fileID)
	if err != nil {
		return "", "", "", err
	}
	meta, _, err := p.loadFile(ctx, channelKey, fileID)
	if err != nil {
		return "", "", "", err
	}
	if meta.Kind != fileKindUpload {
		return "", "", "", errUploadNotFound
	}
	filename = meta.Filename
	if strings.TrimSpace(in.Filename) != "" {
		filename = sanitizeUploadFilename(in.Filename)
	}
	mimeType = meta.MimeType
	if strings.TrimSpace(in.MimeType) != "" {
		mimeType = in.MimeType
	}
	return workRel, mimeType, filename, nil
}
