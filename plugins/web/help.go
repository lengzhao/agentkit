package web

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("web/http-fetch", plugindoc.Doc{
		Summary: "Fetch a URL over HTTP(S) and return it as readable text.",
		ConfigNotes: map[string]string{
			"timeoutSeconds":    "per-request wall clock; defaults to 30",
			"maxBytes":          "body read limit before extraction; defaults to 1 MiB",
			"maxRedirects":      "redirect chain limit; defaults to 5",
			"userAgent":         "override the outgoing User-Agent",
			"allowPrivateHosts": "allow loopback / private / link-local targets; off by default",
			"allowHosts":        "when non-empty, only these hosts (and their subdomains) may be fetched",
			"denyHosts":         "hosts to refuse, applied after allowHosts",
		},
		BestPractices: []string{
			"Needs no credentials, so it is the one web provider that works in a keyless setup.",
			"Leave allowPrivateHosts off: it is what stops a fetched URL from reaching cloud metadata or internal admin endpoints.",
			"The private-address check runs at dial time, so it also covers redirects and DNS rebinding.",
			"Non-text responses are reported as a placeholder instead of being returned as bytes.",
		},
	})
	plugindoc.Register("web/exa-search", plugindoc.Doc{
		Summary: "Search the web through the Exa API.",
		ConfigNotes: map[string]string{
			"apiKeyRef":      `credential ref resolved via deps.credentials, e.g. "env:EXA_API_KEY"`,
			"apiKey":         "literal key; prefer apiKeyRef so the secret stays out of config files",
			"baseUrl":        "override the API host; defaults to https://api.exa.ai",
			"maxResults":     "cap on hits per call; defaults to 5",
			"timeoutSeconds": "per-request wall clock; defaults to 30",
			"type":           "Exa search mode: auto (default), fast, instant, deep",
			"category":       `narrow results, e.g. "news" or "research paper"`,
			"includeText":    "also request page text; costs more tokens and money than highlights alone",
			"snippetChars":   "snippet truncation limit; defaults to 800",
			"includeDomains": "restrict results to these domains",
			"excludeDomains": "drop results from these domains",
		},
		BestPractices: []string{
			"A missing key is reported at call time, not at build time, so mounting search never breaks a keyless preset.",
			"Search returns snippets; pair it with tool/web-fetch when the model needs the full page.",
		},
	})
	plugindoc.Register("web/scripted-search", plugindoc.Doc{
		Summary: "Canned search results for tests and keyless smoke runs.",
		ConfigNotes: map[string]string{
			"results": "hits returned for any query byQuery does not match",
			"byQuery": "case-insensitive query substring to hits; keep keys mutually exclusive",
		},
	})
	plugindoc.Register("web/scripted-fetch", plugindoc.Doc{
		Summary: "Canned page bodies for tests and keyless smoke runs.",
		ConfigNotes: map[string]string{
			"pages":   "URL substring to the HTML served for it",
			"default": "body served when no pages key matches; empty means not found",
		},
	})
}
