package workshop

import (
	"fmt"
	"regexp"
	"strings"
)

const maxSkillBodyChars = 10000

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rm\s+-rf\s+/`),
	regexp.MustCompile(`(?i)curl\s+.*\|\s*(ba)?sh`),
	regexp.MustCompile(`(?i)wget\s+.*\|\s*(ba)?sh`),
}

// ScanResult is the outcome of a proposal safety scan.
type ScanResult struct {
	OK       bool
	Critical []string
	Warn     []string
}

// Scan checks proposal content before apply.
func Scan(name, body string) ScanResult {
	res := ScanResult{OK: true}
	body = strings.TrimSpace(body)
	if name == "" {
		res.OK = false
		res.Critical = append(res.Critical, "skill name is required")
	}
	if !validSkillName(name) {
		res.OK = false
		res.Critical = append(res.Critical, "skill name must be lowercase alphanumeric with hyphens")
	}
	if body == "" {
		res.OK = false
		res.Critical = append(res.Critical, "proposal body is empty")
	}
	if len([]rune(body)) > maxSkillBodyChars {
		res.OK = false
		res.Critical = append(res.Critical, fmt.Sprintf("body exceeds %d characters", maxSkillBodyChars))
	}
	lower := strings.ToLower(body)
	secretNeedles := []string{"sk-", "api_key", "apikey", "private_key", "password="}
	for _, n := range secretNeedles {
		if strings.Contains(lower, n) {
			res.OK = false
			res.Critical = append(res.Critical, "body may contain secrets")
			break
		}
	}
	for _, re := range dangerousPatterns {
		if re.MatchString(body) {
			res.Warn = append(res.Warn, "body matches dangerous shell pattern")
		}
	}
	return res
}

func validSkillName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' && i > 0 {
			continue
		}
		if r == '-' && i > 0 && i < len(name)-1 {
			continue
		}
		return false
	}
	return true
}
