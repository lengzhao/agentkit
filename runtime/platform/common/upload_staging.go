package common

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/lengzhao/agentkit"
)

// stagedUpload is an opaque, process-local owner of one 0600 temporary file.
// No user-supplied path is ever used as a cleanup target.
type stagedUpload struct {
	path     string
	once     sync.Once
	closeErr error
}

func (s *stagedUpload) Open() (io.ReadCloser, error) { return os.Open(s.path) }
func (s *stagedUpload) Close() error {
	s.once.Do(func() {
		s.closeErr = os.Remove(s.path)
		if os.IsNotExist(s.closeErr) {
			s.closeErr = nil
		}
	})
	return s.closeErr
}

func stageUpload(data []byte) (agentkit.InboundUploadSource, error) {
	f, err := os.CreateTemp("", "agentkit-inbound-*")
	if err != nil {
		return nil, fmt.Errorf("create inbound staging file: %w", err)
	}
	source := &stagedUpload{path: f.Name()}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = source.Close()
		if writeErr != nil {
			return nil, fmt.Errorf("write inbound staging file: %w", writeErr)
		}
		return nil, fmt.Errorf("close inbound staging file: %w", closeErr)
	}
	return source, nil
}

// StageInboundUploads takes ownership of transient uploads and replaces byte
// payloads with opaque disk sources before queue retention. On failure it
// releases staging data and bytes; the caller must report the returned error
// rather than dispatch the event.
func StageInboundUploads(event agentkit.MessageEvent) (agentkit.MessageEvent, error) {
	if len(event.Uploads) == 0 {
		return event, nil
	}
	event.Uploads = append([]agentkit.InboundUpload(nil), event.Uploads...)
	event.Message.Content = append([]agentkit.ContentPart(nil), event.Message.Content...)
	for i := range event.Uploads {
		u := &event.Uploads[i]
		if u.Source == nil && len(u.Data) > 0 {
			source, err := stageUpload(u.Data)
			if err != nil {
				ReleaseInboundUploads(event)
				event.Uploads = nil
				return event, err
			}
			u.Source = source
		}
		u.Data = nil
		if u.ContentIndex >= 0 && u.ContentIndex < len(event.Message.Content) && event.Message.Content[u.ContentIndex].Type == "image_url" {
			event.Message.Content[u.ContentIndex].URL = ""
		}
	}
	return event, nil
}

// ReleaseInboundUploads discards only owned transient staging. Final workspace
// files are separate copies and are never removed. Safe on every terminal path.
func ReleaseInboundUploads(event agentkit.MessageEvent) {
	for _, u := range event.Uploads {
		if u.Source != nil {
			if err := u.Source.Close(); err != nil {
				slog.Warn("release inbound staging failed", "error", err)
			}
		}
	}
}
