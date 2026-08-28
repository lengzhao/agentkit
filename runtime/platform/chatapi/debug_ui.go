package chatapi

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed debugui/index.html
var debugUIFS embed.FS

func (p *Platform) registerDebugUI(mux *http.ServeMux) {
	if !p.debugUI {
		return
	}
	content, err := fs.ReadFile(debugUIFS, "debugui/index.html")
	if err != nil {
		return
	}
	html := string(content)
	inject := `<script>window.__CHAT_API_PATH__=` + jsonString(p.path) + `;</script>`
	if strings.Contains(html, "</head>") {
		html = strings.Replace(html, "</head>", inject+"\n</head>", 1)
	} else {
		html = inject + html
	}
	page := []byte(html)

	serve := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeErr(w, http.StatusMethodNotAllowed, "invalid request")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	}

	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})
	mux.HandleFunc("/debug/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/debug/", "/debug/index.html":
			serve(w, r)
		case "/debug/config.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"project":        "platformPlugin",
				"runtime":        "agentkit",
				"api_path":       p.path,
				"user_header":    p.userHeader,
				"channel_header": p.channelHeader,
				"default_agent":  string(p.agentID),
				"agents":         p.availableAgents,
			})
		default:
			http.NotFound(w, r)
		}
	})
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `"/v1/"`
	}
	return string(b)
}
