package rkey

import (
	"encoding/json"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	zero "github.com/wdvxdr1123/ZeroBot"
)

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)
var fieldTokenRe = regexp.MustCompile(`(?i)"(?:rkey|key)"\s*:\s*"([^"]+)"`)
var rkeyParamRe = regexp.MustCompile(`(?i)(?:^|[&?])rkey=([A-Za-z0-9_\-]{8,})`)

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

// GetByType returns rkey for a specific type (e.g. 10/20).
func GetByType(t int) string {
	mu.RLock()
	defer mu.RUnlock()
	return byType[t]
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
	log.Println("[rkey] RefreshFromBots start")
	zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
		if ctx == nil {
			log.Printf("[rkey] bot(%d) ctx nil", id)
			return true
		}
		raw := ctx.NcGetRKey().Raw
		v, changed := UpdateFromRaw(raw)
		if v != "" {
			log.Printf("[rkey] bot(%d) got rkey=%s changed=%v", id, maskKey(v), changed)
			return false
		}
		log.Printf("[rkey] bot(%d) NcGetRKey empty/unparseable raw_len=%d raw_preview=%q",
			id, len(raw), previewRaw(raw))
		return true
	})
	v := Get()
	if v == "" {
		log.Println("[rkey] RefreshFromBots done: no rkey")
		return ""
	}
	log.Printf("[rkey] RefreshFromBots done: selected=%s t10=%s t20=%s",
		maskKey(v), maskKey(GetByType(10)), maskKey(GetByType(20)))
	return v
}

// UpdateFromRaw extracts rkey(s) from API raw JSON/body string and updates cache.
// Returns one extracted key (if any) and whether cache content changed.
func UpdateFromRaw(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	// Some adapters may return a JSON-escaped payload string; unquote once.
	if unq, err := strconv.Unquote(raw); err == nil {
		unq = strings.TrimSpace(unq)
		if unq != "" && unq != raw {
			raw = unq
		}
	}

	entries := parseEntries(raw)
	if len(entries) == 0 {
		// Raw token/url fallback.
		if v := extractToken(raw); v != "" {
			out, changed := setEntry(entry{Key: v})
			log.Printf("[rkey] UpdateFromRaw fallback token=%s changed=%v", maskKey(out), changed)
			return out, changed
		}
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
	if first != "" {
		log.Printf("[rkey] UpdateFromRaw parsed first=%s changed=%v t10=%s t20=%s",
			maskKey(first), changed, maskKey(GetByType(10)), maskKey(GetByType(20)))
	}
	return first, changed
}

func maskKey(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return v
	}
	return v[:4] + "..." + v[len(v)-4:]
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

	// 3) Generic fallback: scan any "rkey"/"key" fields from arbitrary JSON shapes.
	matches := fieldTokenRe.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		out := make([]entry, 0, len(matches))
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			k := extractToken(m[1])
			if k == "" {
				continue
			}
			out = append(out, entry{Key: k})
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
	// Handle raw query-style payloads like "&rkey=xxxx" or "rkey=xxxx&ttl=..."
	if unq, err := url.QueryUnescape(s); err == nil && strings.TrimSpace(unq) != "" {
		s = strings.TrimSpace(unq)
	}
	s = strings.TrimPrefix(s, "?")
	s = strings.TrimPrefix(s, "&")
	if vals, err := url.ParseQuery(s); err == nil {
		if q := vals.Get("rkey"); q != "" && tokenRe.MatchString(q) && len(q) >= 8 {
			return q
		}
	}
	if m := rkeyParamRe.FindStringSubmatch("&" + s); len(m) >= 2 {
		q := strings.TrimSpace(m[1])
		if tokenRe.MatchString(q) && len(q) >= 8 {
			return q
		}
	}
	return ""
}

func previewRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "\n", "\\n")
	raw = strings.ReplaceAll(raw, "\r", "\\r")
	if len(raw) > 180 {
		return raw[:180] + "..."
	}
	return raw
}
