package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sixath/framework/agent"
)

// CacheKeyBuilder 自定义缓存 key 计算。
type CacheKeyBuilder interface {
	BuildKey(req *agent.Request) string
}

// DefaultCacheKey 默认 key：Messages + Parts + 关键 metadata。
type DefaultCacheKey struct {
	Version      int
	MetadataKeys []string
}

func (k *DefaultCacheKey) BuildKey(req *agent.Request) string {
	if req == nil {
		return ""
	}
	version := k.Version
	if version == 0 {
		version = 1
	}
	metaKeys := k.MetadataKeys
	if len(metaKeys) == 0 {
		metaKeys = []string{"model", "temperature", "system"}
	}

	h := sha256.New()
	fmt.Fprintf(h, "v=%d;", version)
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		fmt.Fprintf(h, "role=%s;", role)
		fmt.Fprintf(h, "content=%s;", m.Content)
		for _, p := range m.Parts {
			fmt.Fprintf(h, "part_type=%s;", p.Type)
			fmt.Fprintf(h, "part_text=%s;", p.Text)
			fmt.Fprintf(h, "part_url=%s;", p.URL)
		}
		h.Write([]byte("|"))
	}
	keys := append([]string(nil), metaKeys...)
	sort.Strings(keys)
	if req.Metadata != nil {
		for _, mk := range keys {
			v, ok := req.Metadata[mk]
			if !ok {
				continue
			}
			b, err := json.Marshal(v)
			if err != nil {
				fmt.Fprintf(h, "%s=%v;", mk, v)
			} else {
				fmt.Fprintf(h, "%s=%s;", mk, b)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func cacheKeyForRequest(req *agent.Request) string {
	return (&DefaultCacheKey{Version: 1}).BuildKey(req)
}
