package deferred

import (
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

// searchCatalog ranks tools using token overlap, BM25, raw substring, and CJK 2-gram signals combined.
func searchCatalog(catalog []catalogEntry, query string, limit int) []agentkit.ToolSpec {
	if limit <= 0 {
		limit = 5
	}
	q := strings.TrimSpace(query)
	if q == "" || len(catalog) == 0 {
		return nil
	}

	bm25 := buildBM25Index(catalog)
	bm25Scores := bm25.scoresForQuery(q)
	qLower := strings.ToLower(q)
	pureCJK := isPureCJKQuery(q)

	type ranked struct {
		spec  agentkit.ToolSpec
		score float64
	}
	hits := make([]ranked, 0, len(catalog))
	for i, entry := range catalog {
		sub := scoreSubstring(qLower, entry)
		if pureCJK {
			sub = max(sub, scoreCJKSubstring(q, entry))
		}
		combined := combineSearchScores(
			float64(scoreQuery(q, entry)),
			bm25Scores[i],
			sub,
			scoreCJK2Gram(q, entry),
			pureCJK,
		)
		if combined <= 0 {
			continue
		}
		hits = append(hits, ranked{spec: entry.Spec, score: combined})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].spec.Name < hits[j].spec.Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]agentkit.ToolSpec, len(hits))
	for i, h := range hits {
		out[i] = h.spec
	}
	return out
}

func combineSearchScores(tokenScore, bm25Score, substringScore, cjk2Score float64, pureCJK bool) float64 {
	// Union: any signal can surface a tool; ranking uses weighted sum of normalized legs.
	if tokenScore <= 0 && bm25Score <= 0 && substringScore <= 0 && cjk2Score <= 0 {
		return 0
	}
	subW := 15.0
	cjk2W := 6.0
	if pureCJK {
		subW = 28.0
		cjk2W = 14.0
	}
	return tokenScore*12 + bm25Score*8 + substringScore*subW + cjk2Score*cjk2W
}

// scoreSubstring catches queries that token/BM25 miss (punctuation, partial names, CJK adjacent ASCII).
func scoreSubstring(qLower string, entry catalogEntry) float64 {
	if len(qLower) < 2 {
		return 0
	}
	nameLower := strings.ToLower(entry.Spec.Name)
	textLower := strings.ToLower(entry.SearchText)
	switch {
	case strings.Contains(nameLower, qLower):
		return 3
	case strings.Contains(textLower, qLower):
		return 2
	default:
		// Also try query tokens glued (e.g. "github search" -> githubsearch unlikely; "mcp ping" in text)
		parts := tokenize(qLower)
		if len(parts) >= 2 {
			joined := strings.Join(parts, "")
			if strings.Contains(nameLower, joined) || strings.Contains(textLower, joined) {
				return 1.5
			}
		}
	}
	return 0
}
