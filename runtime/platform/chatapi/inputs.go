package chatapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type chatInput struct {
	Type           string `json:"type"`
	TransferMethod string `json:"transfer_method"`
	Data           string `json:"data"`
	Path           string `json:"path"`
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
		case "local_path":
			workRel, mimeType, filename, err = normalizeWorkspaceInputPath(in)
			if err != nil {
				return nil, nil, nil, nil, err
			}
		default:
			return nil, nil, nil, nil, fmt.Errorf("unsupported transfer_method")
		}
		switch strings.ToLower(in.Type) {
		case "image":
			if len(data) == 0 && workRel != "" {
				data, err = p.readWorkFile(ctx, channelKey, workRel)
				if err != nil {
					return nil, nil, nil, nil, err
				}
			}
			images = append(images, common.ImageAttachment{
				MimeType: mimeType, Data: data, FileName: filename, WorkPath: workRel,
			})
		case "file":
			if common.IsImageAttachment(mimeType, filename) {
				if len(data) == 0 && workRel != "" {
					data, err = p.readWorkFile(ctx, channelKey, workRel)
					if err != nil {
						return nil, nil, nil, nil, err
					}
				}
				if len(data) > 0 {
					images = append(images, common.ImageAttachment{
						MimeType: mimeType, Data: data, FileName: filename, WorkPath: workRel,
					})
					continue
				}
			}
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
				data, err = p.readWorkFile(ctx, channelKey, workRel)
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

func normalizeWorkspaceInputPath(in chatInput) (workRel, mimeType, filename string, err error) {
	workRel = strings.TrimSpace(in.Path)
	if workRel == "" {
		workRel = strings.TrimSpace(in.Data)
	}
	if workRel == "" {
		return "", "", "", fmt.Errorf("path required for local_path")
	}
	workRel = filepath.ToSlash(workRel)
	workRel = strings.TrimPrefix(workRel, "/")
	workRel = strings.TrimPrefix(workRel, "work/")
	if strings.Contains(workRel, "..") {
		return "", "", "", fmt.Errorf("invalid path")
	}
	filename = filepath.Base(workRel)
	if strings.TrimSpace(in.Filename) != "" {
		filename = sanitizeUploadFilename(in.Filename)
	}
	mimeType = in.MimeType
	if strings.TrimSpace(mimeType) == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(filename))
	}
	return workRel, mimeType, filename, nil
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

func (p *Platform) readWorkFile(ctx context.Context, channelKey, workRel string) ([]byte, error) {
	abs, err := p.workspace.Resolve(p.channelCtx(ctx, channelKey), "work/"+workRel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}
