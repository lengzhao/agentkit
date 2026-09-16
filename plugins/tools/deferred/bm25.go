package deferred

import (
	"math"
)

const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

type bm25Index struct {
	docTokens [][]string
	idf       map[string]float64
	avgDL     float64
}

func buildBM25Index(catalog []catalogEntry) *bm25Index {
	n := len(catalog)
	if n == 0 {
		return &bm25Index{}
	}
	docTokens := make([][]string, n)
	df := map[string]int{}
	var totalLen float64
	for i, e := range catalog {
		toks := stemTokens(tokenize(e.SearchText))
		docTokens[i] = toks
		totalLen += float64(len(toks))
		seen := map[string]bool{}
		for _, t := range toks {
			if seen[t] {
				continue
			}
			seen[t] = true
			df[t]++
		}
	}
	avgDL := totalLen / float64(n)
	if avgDL < 1 {
		avgDL = 1
	}
	idf := make(map[string]float64, len(df))
	for term, freq := range df {
		// Robertson–Spark Jones IDF variant (common in search engines).
		idf[term] = math.Log(1 + (float64(n)-float64(freq)+0.5)/(float64(freq)+0.5))
	}
	return &bm25Index{docTokens: docTokens, idf: idf, avgDL: avgDL}
}

func (idx *bm25Index) scoreDoc(docIdx int, queryTerms []string) float64 {
	if idx == nil || docIdx < 0 || docIdx >= len(idx.docTokens) {
		return 0
	}
	toks := idx.docTokens[docIdx]
	if len(toks) == 0 || len(queryTerms) == 0 {
		return 0
	}
	tf := map[string]int{}
	for _, t := range toks {
		tf[t]++
	}
	dl := float64(len(toks))
	var score float64
	seenQ := map[string]bool{}
	for _, qt := range queryTerms {
		if qt == "" || seenQ[qt] {
			continue
		}
		seenQ[qt] = true
		f := float64(tf[qt])
		if f == 0 {
			continue
		}
		idf := idx.idf[qt]
		denom := f + bm25K1*(1-bm25B+bm25B*dl/idx.avgDL)
		score += idf * (f * (bm25K1 + 1)) / denom
	}
	return score
}

func (idx *bm25Index) scoresForQuery(query string) []float64 {
	qTerms := stemTokens(tokenize(query))
	out := make([]float64, len(idx.docTokens))
	for i := range idx.docTokens {
		out[i] = idx.scoreDoc(i, qTerms)
	}
	return out
}
