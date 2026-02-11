package rkey

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"sync"

	zero "github.com/wdvxdr1123/ZeroBot"
)

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)

type entry struct {
	Type int
	Key  string
}

var (
	mu      sync.RWMutex
	byType  = map[int]string{}
	fallback string
)

// Get returns a preferred rkey (type=10 first, then type=20, then fallback).
func Get() string {
	mu.RLock()
	defer mu.RUnlock()
	if v := byType[10]; v != "" {
		return v
	}
	if v := byType[20]; v != "" {
		return v
	}
	return fallback
}

// CandidatesForURL returns rkey candidates ordered by URL resource kind.
func CandidatesForURL(rawURL string) []string {
	rawURL = strings.TrimSpace(rawURL)
	low := strings.ToLower(rawURL)
	// Default: type=10 first (images, short media), then type=20 (large/offline files).
	order := []int{10, 20}
	// Common large-file/offline patterns: prefer type=20 first.
	if strings.Contains(low, "weiyun") || strings.Contains(low, "offline") || strings.Contains(low, "ftn") {
		order = []int{20, 10}
	}
	return candidatesByOrder(order)
}

// RefreshFromBots tries to fetch rkey from any online bot context via NcGetRKey.
func RefreshFromBots() string {
	zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
		if ctx == nil {
			return true
		}
		v, _ := UpdateFromRaw(ctx.NcGetRKey().Raw)
		if v != "" {
			return false
		}
		return true
	})
	return Get()
}

// UpdateFromRaw extracts rkey(s) from API raw JSON/body string and updates cache.
// Returns one extracted key (if any) and whether cache content changed.
func UpdateFromRaw(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	// Raw token/url fallback.
	if v := extractToken(raw); v != "" {
		return setEntry(entry{Key: v})
	}

	entries := parseEntries(raw)
	if len(entries) == 0 {
		return "", false
	}

	first := ""
	changed := false
	for _, e := range entries {
		if e.Key == "" {
			continue
		}
		if first == "" {
			first = e.Key
		}
		_, ch := setEntry(e)
		if ch {
			changed = true
		}
	}
	return first, changed
}

func candidatesByOrder(order []int) []string {
	mu.RLock()
	defer mu.RUnlock()

	res := make([]string, 0, len(byType)+1)
	seen := map[string]struct{}{}
	push := func(v string) {
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		res = append(res, v)
	}

	for _, t := range order {
		push(byType[t])
	}
	for _, v := range byType {
		push(v)
	}
	push(fallback)
	return res
}

func setEntry(e entry) (string, bool) {
	if e.Key == "" {
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()

	changed := false
	if e.Type != 0 {
		if byType[e.Type] != e.Key {
			byType[e.Type] = e.Key
			changed = true
		}
	}
	if fallback != e.Key {
		fallback = e.Key
		changed = true
	}
	return e.Key, changed
}

func parseEntries(raw string) []entry {
	// 1) NcGetRKey data raw: [{"type":"private","rkey":"...","ttl":...}, ...]
	var arr []struct {
		Type interface{} `json:"type"`
		RKey string      `json:"rkey"`
		Key  string      `json:"key"`
	}
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		out := make([]entry, 0, len(arr))
		for _, it := range arr {
			k := extractToken(firstNonEmpty(it.RKey, it.Key))
			if k == "" {
				continue
			}
			out = append(out, entry{Type: normalizeType(it.Type), Key: k})
		}
		if len(out) > 0 {
			return out
		}
	}

	// 2) Wrapper shape: {"data":[{"key":"..."}]} or {"data":[{"rkey":"..."}]}
	var obj struct {
		Data []struct {
			Type interface{} `json:"type"`
			RKey string      `json:"rkey"`
			Key  string      `json:"key"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil && len(obj.Data) > 0 {
		out := make([]entry, 0, len(obj.Data))
		for _, it := range obj.Data {
			k := extractToken(firstNonEmpty(it.RKey, it.Key))
			if k == "" {
				continue
			}
			out = append(out, entry{Type: normalizeType(it.Type), Key: k})
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func normalizeType(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		switch s {
		case "10", "image", "media", "private":
			return 10
		case "20", "file", "offline", "group":
			return 20
		default:
			return 0
		}
	default:
		return 0
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func extractToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if tokenRe.MatchString(s) && len(s) >= 8 {
		return s
	}
	if u, err := url.Parse(s); err == nil {
		if q := u.Query().Get("rkey"); q != "" && tokenRe.MatchString(q) && len(q) >= 8 {
			return q
		}
	}
	return ""
}

