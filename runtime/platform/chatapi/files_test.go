package chatapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestUploadAndChatWithLocalFile(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	p, err := New(Config{}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	channel := "ch-files"
	user := "u1"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello upload")); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Chat-API-Channel", channel)
	req.Header.Set("X-Chat-API-User", user)
	rec := httptest.NewRecorder()
	plat.handleUploadFile(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status %d body %s", rec.Code, rec.Body.String())
	}
	var uploadResp struct {
		OK   bool `json:"ok"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatal(err)
	}
	if !uploadResp.OK || uploadResp.Data.ID == "" {
		t.Fatalf("upload response %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"url":`) {
		t.Fatalf("upload response missing url: %s", rec.Body.String())
	}

	inputs := []chatInput{{
		Type:           "file",
		TransferMethod: "local_file",
		UploadFileID:   uploadResp.Data.ID,
	}}
	images, files, audio, paths, err := plat.inputsToCore(context.Background(), channel, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 0 || len(files) != 0 || audio != nil || len(paths) != 1 {
		t.Fatalf("inputsToCore = images=%d files=%d audio=%v paths=%v", len(images), len(files), audio, paths)
	}
	if paths[0] != "work/upload/"+uploadResp.Data.ID+".note.txt" {
		// filename is embedded in managed name
		if !strings.HasPrefix(paths[0], "work/upload/") || !strings.HasSuffix(paths[0], ".note.txt") {
			t.Fatalf("path = %q", paths[0])
		}
	}

	event := common.InboundFromContent(
		"assistant",
		session.SessionRouteInput{
			Platform:   "chat-api",
			DeliveryID: agentkit.SessionID("chat-api:" + channel + ":t:conv1"),
			ScopeUserID: user,
		},
		user,
		"read the file",
		"",
		nil, nil, nil, paths,
		nil,
	)
	text := event.Message.Content[0].Text
	if !strings.Contains(text, "work/upload/") {
		t.Fatalf("prompt missing upload ref: %q", text)
	}

	uploadDir, err := ws.Resolve(context.Background(), common.UploadWorkRel())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected content+meta in %s, got %d entries", uploadDir, len(entries))
	}
}

func TestUploadAndChatWithLocalImageFile(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	p, err := New(Config{}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	channel := "ch-images"
	user := "u1"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "asset.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Chat-API-Channel", channel)
	req.Header.Set("X-Chat-API-User", user)
	rec := httptest.NewRecorder()
	plat.handleUploadFile(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status %d body %s", rec.Code, rec.Body.String())
	}
	var uploadResp struct {
		OK   bool `json:"ok"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatal(err)
	}

	inputs := []chatInput{{
		Type:           "file",
		TransferMethod: "local_file",
		UploadFileID:   uploadResp.Data.ID,
	}}
	images, files, audio, paths, err := plat.inputsToCore(context.Background(), channel, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || len(files) != 0 || audio != nil || len(paths) != 0 {
		t.Fatalf("inputsToCore = images=%d files=%d audio=%v paths=%v", len(images), len(files), audio, paths)
	}
	if images[0].WorkPath == "" {
		t.Fatal("expected work path on uploaded image")
	}
	if len(images[0].Data) == 0 {
		t.Fatal("expected image bytes")
	}

	event := common.InboundFromContent(
		"assistant",
		session.SessionRouteInput{
			Platform:   "chat-api",
			DeliveryID: agentkit.SessionID("chat-api:" + channel + ":t:conv1"),
			ScopeUserID: user,
		},
		user,
		"extract assets",
		"",
		images, nil, nil, paths,
		common.InboundOptsFor(ws),
	)
	if len(event.Message.Content) < 2 {
		t.Fatalf("content parts = %d, want text + image", len(event.Message.Content))
	}
	if event.Message.Content[1].Type != "image_url" || event.Message.Content[1].URL == "" {
		t.Fatalf("image part = %#v", event.Message.Content[1])
	}
	if event.Message.Content[1].Source == "" {
		t.Fatal("expected workspace path in image source")
	}
	if strings.Contains(event.Message.Content[0].Text, "please read them") {
		t.Fatalf("image should not be routed through read tool hint: %q", event.Message.Content[0].Text)
	}
}

func TestInputsToCoreLocalPathImage(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "pic.png"), []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, 0o644); err != nil {
		t.Fatal(err)
	}
	plat, err := New(Config{}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := plat.(*Platform)
	images, _, _, _, err := p.inputsToCore(context.Background(), "ch", []chatInput{{
		Type:           "image",
		TransferMethod: "local_path",
		Path:           "upload/pic.png",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].WorkPath != "work/upload/pic.png" || len(images[0].Data) == 0 {
		t.Fatalf("images = %#v", images)
	}
}

func TestDownloadUploadedFile(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	plat, err := New(Config{}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := plat.(*Platform)
	channel := "ch-dl"

	meta, err := p.saveUploadedFile(context.Background(), channel, "u1", "data.bin", "application/octet-stream", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/files/"+meta.ID+"?channel="+channel, nil)
	req.Header.Set("X-Chat-API-Channel", channel)
	rec := httptest.NewRecorder()
	p.handleFileRoutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "payload" {
		t.Fatalf("body = %q", got)
	}
}

func TestDownloadUploadedFileChannelQueryOnly(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	plat, err := New(Config{APIToken: "secret"}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := plat.(*Platform)
	channel := "ch-dl"

	meta, err := p.saveUploadedFile(context.Background(), channel, "u1", "data.bin", "application/octet-stream", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/files/"+meta.ID+"?channel="+channel, nil)
	rec := httptest.NewRecorder()
	p.handleFileRoutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestFileDownloadSkipsAPIToken(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	plat, err := New(Config{APIToken: "secret", Path: "/v1/"}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := plat.(*Platform)
	channel := "ch-dl"
	meta, err := p.saveUploadedFile(context.Background(), channel, "u1", "note.txt", "text/plain", []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/files/"+meta.ID+"?channel="+channel, nil)
	rec := httptest.NewRecorder()
	p.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestEmitAssistantMediaFileReady(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	plat, err := New(Config{}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := plat.(*Platform)

	workDir, err := ws.Resolve(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(workDir, "out.txt")
	if err := os.WriteFile(filePath, []byte("sent file"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState("run1", "u1", "ch1", "", "sess", "conv", "msg1", p, sse)
	run.apiBase = "http://example.com/v1"

	msg := agentkit.ModelMessage{
		Content: []agentkit.ContentPart{{
			Type: "document",
			URL:  filePath,
			MIME: "text/plain",
		}},
	}
	if err := p.emitAssistantMedia(context.Background(), run, msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "file_ready") {
		t.Fatalf("expected file_ready SSE, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "http://example.com/v1/files/") {
		t.Fatalf("expected download url in SSE, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "channel=ch1") {
		t.Fatalf("expected channel in download url, got %s", rec.Body.String())
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	plat, err := New(Config{MaxUploadSize: 16}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := plat.(*Platform)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "big.bin")
	_, _ = io.Copy(part, bytes.NewReader(make([]byte, 32)))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Chat-API-Channel", "ch")
	req.Header.Set("X-Chat-API-User", "u1")
	rec := httptest.NewRecorder()
	p.handleUploadFile(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
