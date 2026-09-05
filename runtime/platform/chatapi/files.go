package chatapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/runtime/platform/common"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

const (
	fileIDPrefix         = "file_"
	uploadMetaSuffix     = ".meta.json"
	fileKindUpload       = "upload"
	fileKindDownload     = "download"
	defaultMaxUploadSize = 10 << 20
)

var (
	fileIDPattern        = regexp.MustCompile(`^file_[A-Za-z0-9_-]{22}$`)
	errUploadNotFound    = errors.New("upload not found")
	errWorkspaceRequired = errors.New("workspace not configured")
	errInvalidPath       = errors.New("invalid path")
	errPathOutsideWork   = errors.New("path outside work")
	errForbidden         = errors.New("forbidden")
)

type uploadedFileMeta struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Channel   string `json:"channel"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	UserID    string `json:"user_id,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

type fileView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"`
	UserID    string `json:"user_id,omitempty"`
	Path      string `json:"path"`
	URL       string `json:"url,omitempty"`
}

func (p *Platform) channelCtx(ctx context.Context, channelKey string) context.Context {
	return channelWorkspaceCtx(ctx, channelKey)
}

func (p *Platform) uploadDir(ctx context.Context, channelKey string) (string, error) {
	if p.workspace == nil {
		return "", errWorkspaceRequired
	}
	dir, err := p.workspace.Resolve(p.channelCtx(ctx, channelKey), common.UploadWorkRel())
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (p *Platform) downloadDir(ctx context.Context, channelKey string) (string, error) {
	if p.workspace == nil {
		return "", errWorkspaceRequired
	}
	dir, err := p.workspace.Resolve(p.channelCtx(ctx, channelKey), "work/download")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (p *Platform) workRelPath(ctx context.Context, channelKey, absPath string) (string, error) {
	root, err := p.workspace.Resolve(p.channelCtx(ctx, channelKey), ".")
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside workspace")
	}
	return filepath.ToSlash(rel), nil
}

func (p *Platform) handleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		p.handleListFiles(w, r)
	case http.MethodPost:
		p.handleUploadFile(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
	}
}

func (p *Platform) handleFileRoutes(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, p.path+"files/")
	sub = strings.Trim(strings.TrimSpace(sub), "/")
	if sub == "" {
		p.handleFiles(w, r)
		return
	}
	if !fileIDPattern.MatchString(sub) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	channelKey, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		p.handleDownloadFile(w, r, channelKey, sub)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
	}
}

func (p *Platform) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if path := strings.TrimSpace(r.URL.Query().Get("path")); path != "" {
		p.handleDownloadByPath(w, r, path)
		return
	}
	channelKey, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	switch kind {
	case "", "all":
		kind = "all"
	case fileKindUpload, fileKindDownload:
	default:
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	files, hasMore, nextCursor, err := p.listFiles(r.Context(), channelKey, kind, limit, p.apiBaseFromRequest(r))
	if err != nil {
		if errors.Is(err, errWorkspaceRequired) {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		slog.Error("chat-api: list files", "channel", channelKey, "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	data := map[string]any{
		"limit":    clampLimit(limit),
		"has_more": hasMore,
		"files":    files,
	}
	if nextCursor != "" {
		data["next_cursor"] = nextCursor
	}
	writeOK(w, http.StatusOK, data)
}

func (p *Platform) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	user, ok := p.resolveUser(w, r, true)
	if !ok {
		return
	}
	channelKey, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(p.maxUploadSize); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, p.maxUploadSize+1))
	if err != nil {
		slog.Error("chat-api: read upload", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if int64(len(data)) > p.maxUploadSize {
		writeErr(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}

	filename := sanitizeUploadFilename(header.Filename)
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if formMime := strings.TrimSpace(r.FormValue("mime_type")); formMime != "" {
		mimeType = formMime
	}

	if formPath := strings.TrimSpace(r.FormValue("path")); formPath != "" {
		abs, displayPath, err := p.resolveFileAPIPath(r.Context(), channelKey, user, formPath)
		if err != nil {
			if errors.Is(err, errForbidden) {
				writeErr(w, http.StatusForbidden, "forbidden")
				return
			}
			if errors.Is(err, errInvalidPath) || errors.Is(err, errPathOutsideWork) {
				writeErr(w, http.StatusBadRequest, "invalid request")
				return
			}
			if errors.Is(err, errWorkspaceRequired) {
				writeErr(w, http.StatusInternalServerError, "internal error")
				return
			}
			slog.Error("chat-api: resolve upload path", "path", formPath, "error", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if formName := sanitizeUploadFilename(r.FormValue("filename")); formName != "" {
			filename = formName
		} else if filename == "" {
			filename = filepath.Base(displayPath)
		}
		info, err := p.saveFileAtAbsPath(abs, displayPath, filename, mimeType, data)
		if err != nil {
			slog.Error("chat-api: save upload at path", "path", displayPath, "error", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeOK(w, http.StatusOK, map[string]any{
			"path":       info.Path,
			"filename":   info.Filename,
			"mime_type":  info.MimeType,
			"size":       info.Size,
			"created_at": info.CreatedAt,
		})
		return
	}

	meta, err := p.saveUploadedFile(r.Context(), channelKey, user, filename, mimeType, data)
	if err != nil {
		if errors.Is(err, errWorkspaceRequired) {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		slog.Error("chat-api: save upload", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeOK(w, http.StatusCreated, mergeFilePayload(map[string]any{
		"id":         meta.ID,
		"kind":       meta.Kind,
		"filename":   meta.Filename,
		"mime_type":  meta.MimeType,
		"size":       meta.Size,
		"created_at": meta.CreatedAt,
	}, p.fileLinkFields(p.apiBaseFromRequest(r), channelKey, meta.ID)))
}

func (p *Platform) handleDownloadByPath(w http.ResponseWriter, r *http.Request, rawPath string) {
	channelKey, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	user := optionalUser(r, p.userHeader)
	abs, displayPath, err := p.resolveFileAPIPath(r.Context(), channelKey, user, rawPath)
	if err != nil {
		if errors.Is(err, errForbidden) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, errInvalidPath) || errors.Is(err, errPathOutsideWork) {
			writeErr(w, http.StatusBadRequest, "invalid request")
			return
		}
		if errors.Is(err, errWorkspaceRequired) {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		slog.Error("chat-api: resolve download path", "path", rawPath, "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	info, data, err := p.loadFileAtAbsPath(abs, displayPath)
	if err != nil {
		if errors.Is(err, errUploadNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		slog.Error("chat-api: download by path", "path", displayPath, "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if mt := strings.TrimSpace(info.MimeType); mt != "" {
		w.Header().Set("Content-Type", mt)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (p *Platform) handleDownloadFile(w http.ResponseWriter, r *http.Request, channelKey, fileID string) {
	meta, data, err := p.loadFile(r.Context(), channelKey, fileID)
	if err != nil {
		if errors.Is(err, errUploadNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		slog.Error("chat-api: download file", "id", fileID, "error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if mt := strings.TrimSpace(meta.MimeType); mt != "" {
		w.Header().Set("Content-Type", mt)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (p *Platform) saveUploadedFile(ctx context.Context, channelKey, userID, filename, mimeType string, data []byte) (*uploadedFileMeta, error) {
	dir, err := p.uploadDir(ctx, channelKey)
	if err != nil {
		return nil, err
	}
	return p.writeFileRecord(dir, channelKey, fileKindUpload, userID, filename, mimeType, data)
}

func (p *Platform) saveDownloadFile(ctx context.Context, channelKey, filename, mimeType string, data []byte) (*uploadedFileMeta, error) {
	dir, err := p.downloadDir(ctx, channelKey)
	if err != nil {
		return nil, err
	}
	return p.writeFileRecord(dir, channelKey, fileKindDownload, "", filename, mimeType, data)
}

func (p *Platform) writeFileRecord(dir, channelKey, kind, userID, filename, mimeType string, data []byte) (*uploadedFileMeta, error) {
	id, err := newFileID()
	if err != nil {
		return nil, err
	}
	meta := &uploadedFileMeta{
		ID:        id,
		Kind:      kind,
		Channel:   channelKey,
		Filename:  filename,
		MimeType:  mimeType,
		Size:      int64(len(data)),
		UserID:    userID,
		CreatedAt: time.Now().Unix(),
	}
	contentPath := filepath.Join(dir, managedContentBaseName(id, filename))
	metaPath := contentPath + uploadMetaSuffix
	if err := os.WriteFile(contentPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write content: %w", err)
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		_ = os.Remove(contentPath)
		return nil, err
	}
	if err := os.WriteFile(metaPath, raw, 0o644); err != nil {
		_ = os.Remove(contentPath)
		return nil, fmt.Errorf("write meta: %w", err)
	}
	return meta, nil
}

func (p *Platform) loadFile(ctx context.Context, channelKey, fileID string) (*uploadedFileMeta, []byte, error) {
	if !fileIDPattern.MatchString(fileID) {
		return nil, nil, errUploadNotFound
	}
	for _, kindDir := range []struct {
		kind string
		dir  func(context.Context, string) (string, error)
	}{
		{fileKindUpload, p.uploadDir},
		{fileKindDownload, p.downloadDir},
	} {
		dir, err := kindDir.dir(ctx, channelKey)
		if errors.Is(err, errWorkspaceRequired) {
			return nil, nil, err
		}
		if err != nil {
			return nil, nil, err
		}
		meta, data, err := readFileRecord(dir, fileID)
		if errors.Is(err, errUploadNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		return meta, data, nil
	}
	return nil, nil, errUploadNotFound
}

func readFileRecord(dir, fileID string) (*uploadedFileMeta, []byte, error) {
	contentPath, metaPath, err := findManagedFilePaths(dir, fileID)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil, err
	}
	var meta uploadedFileMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(contentPath)
	if err != nil {
		return nil, nil, err
	}
	return &meta, data, nil
}

func (p *Platform) listFiles(ctx context.Context, channelKey, kind string, limit int, apiBase string) ([]fileView, bool, string, error) {
	limit = clampLimit(limit)
	var metas []uploadedFileMeta
	if kind == "all" || kind == fileKindUpload {
		dir, err := p.uploadDir(ctx, channelKey)
		if err != nil && !errors.Is(err, errWorkspaceRequired) {
			return nil, false, "", err
		}
		if err == nil {
			items, err := listFileMetasInDir(dir, fileKindUpload)
			if err != nil {
				return nil, false, "", err
			}
			metas = append(metas, items...)
		}
	}
	if kind == "all" || kind == fileKindDownload {
		dir, err := p.downloadDir(ctx, channelKey)
		if err != nil && !errors.Is(err, errWorkspaceRequired) {
			return nil, false, "", err
		}
		if err == nil {
			items, err := listFileMetasInDir(dir, fileKindDownload)
			if err != nil {
				return nil, false, "", err
			}
			metas = append(metas, items...)
		}
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].CreatedAt > metas[j].CreatedAt
	})
	hasMore := len(metas) > limit
	if hasMore {
		metas = metas[:limit]
	}
	out := make([]fileView, len(metas))
	for i, m := range metas {
		links := p.fileLinkFields(apiBase, channelKey, m.ID)
		out[i] = fileView{
			ID: m.ID, Kind: m.Kind, Filename: m.Filename,
			MimeType: m.MimeType, Size: m.Size, CreatedAt: m.CreatedAt, UserID: m.UserID,
			Path: links["path"], URL: links["url"],
		}
	}
	var next string
	if hasMore && len(out) > 0 {
		next = out[len(out)-1].ID
	}
	return out, hasMore, next, nil
}

func listFileMetasInDir(dir, kind string) ([]uploadedFileMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []uploadedFileMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), uploadMetaSuffix) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var meta uploadedFileMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		if kind != "" && meta.Kind != kind {
			continue
		}
		out = append(out, meta)
	}
	return out, nil
}

func findManagedFilePaths(dir, fileID string) (contentPath, metaPath string, err error) {
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", "", errUploadNotFound
		}
		return "", "", readErr
	}
	prefix := fileID + "."
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, uploadMetaSuffix) {
			continue
		}
		base := strings.TrimSuffix(name, uploadMetaSuffix)
		if base == fileID {
			continue
		}
		candidateMeta := filepath.Join(dir, name)
		candidateContent := strings.TrimSuffix(candidateMeta, uploadMetaSuffix)
		if _, statErr := os.Stat(candidateContent); statErr != nil {
			continue
		}
		candidates = append(candidates, candidateMeta)
	}
	if len(candidates) > 0 {
		sort.Strings(candidates)
		metaPath = candidates[0]
		contentPath = strings.TrimSuffix(metaPath, uploadMetaSuffix)
		return contentPath, metaPath, nil
	}
	return "", "", errUploadNotFound
}

func newFileID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fileIDPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func managedContentBaseName(id, filename string) string {
	name := sanitizeUploadFilename(filename)
	if name == "" {
		name = "file"
	}
	return id + "." + name
}

func sanitizeUploadFilename(name string) string {
	name = filepath.ToSlash(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

type workspaceFileInfo struct {
	Path      string
	Filename  string
	MimeType  string
	Size      int64
	CreatedAt int64
}

func normalizeWorkspaceFilePath(raw string) (string, error) {
	workRel := strings.TrimSpace(raw)
	if workRel == "" {
		return "", errInvalidPath
	}
	workRel = filepath.ToSlash(workRel)
	workRel = strings.TrimPrefix(workRel, "/")
	if strings.Contains(workRel, "..") {
		return "", errInvalidPath
	}
	if workRel == "upload" || strings.HasPrefix(workRel, "upload/") ||
		workRel == "download" || strings.HasPrefix(workRel, "download/") {
		workRel = "work/" + workRel
	}
	return workRel, nil
}

func validateWorkspaceFileAPIPath(workRel string) error {
	workRel = filepath.ToSlash(workRel)
	if !strings.HasPrefix(workRel, "work/") {
		return errPathOutsideWork
	}
	return nil
}

func (p *Platform) resolveFileAPIPath(ctx context.Context, channelKey, userID, rawPath string) (abs, displayPath string, err error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", "", errInvalidPath
	}
	var resolvePath string
	_, _, scoped := rtworkspace.ParseScoped(rawPath)
	switch {
	case rawPath == "~" || strings.HasPrefix(rawPath, "~/") || filepath.IsAbs(rawPath):
		if !p.isAdminUser(userID) {
			return "", "", errForbidden
		}
		resolvePath = rawPath
	case scoped:
		if !p.isAdminUser(userID) {
			return "", "", errForbidden
		}
		resolvePath = rawPath
	default:
		resolvePath, err = normalizeWorkspaceFilePath(rawPath)
		if err != nil {
			return "", "", err
		}
		if err := validateWorkspaceFileAPIPath(resolvePath); err != nil && !p.isAdminUser(userID) {
			return "", "", errForbidden
		}
	}
	if p.workspace == nil {
		return "", "", errWorkspaceRequired
	}
	abs, err = p.workspace.Resolve(p.channelCtx(ctx, channelKey), resolvePath)
	if err != nil {
		return "", "", err
	}
	return abs, filepath.ToSlash(resolvePath), nil
}

func (p *Platform) saveFileAtAbsPath(abs, displayPath, filename, mimeType string, data []byte) (*workspaceFileInfo, error) {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return nil, err
	}
	if filename == "" {
		filename = filepath.Base(displayPath)
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = http.DetectContentType(data)
	}
	return &workspaceFileInfo{
		Path:      filepath.ToSlash(displayPath),
		Filename:  filename,
		MimeType:  mimeType,
		Size:      int64(len(data)),
		CreatedAt: time.Now().Unix(),
	}, nil
}

func (p *Platform) loadFileAtAbsPath(abs, displayPath string) (*workspaceFileInfo, []byte, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, errUploadNotFound
		}
		return nil, nil, err
	}
	filename := filepath.Base(displayPath)
	if filename == "" || filename == "." {
		filename = filepath.Base(abs)
	}
	mimeType := http.DetectContentType(data)
	return &workspaceFileInfo{
		Path:      filepath.ToSlash(displayPath),
		Filename:  filename,
		MimeType:  mimeType,
		Size:      int64(len(data)),
		CreatedAt: fileModTime(abs),
	}, data, nil
}

func fileModTime(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return time.Now().Unix()
	}
	return info.ModTime().Unix()
}

func (p *Platform) uploadedWorkPath(ctx context.Context, channelKey, fileID string) (string, error) {
	meta, _, err := p.loadFile(ctx, channelKey, fileID)
	if err != nil {
		return "", err
	}
	dir, err := p.uploadDir(ctx, channelKey)
	if err != nil {
		return "", err
	}
	contentPath, _, err := findManagedFilePaths(dir, meta.ID)
	if err != nil {
		return "", err
	}
	return p.workRelPath(ctx, channelKey, contentPath)
}
