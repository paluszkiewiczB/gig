package gig

import (
	"fmt"
	"strconv"
)

type segment struct {
	key     string
	isIndex bool
	index   int
}

// YamlKey is a canonical dot-separated path to a YAML node, such as
// "database.host" or "servers[0].host".
type YamlKey string

// Key returns a new YamlKey with the field name appended.
func (k YamlKey) Key(name string) YamlKey {
	if k == "" {
		return YamlKey(name)
	}
	return YamlKey(string(k) + "." + name)
}

// Index returns a new YamlKey with the sequence index appended.
func (k YamlKey) Index(idx int) YamlKey {
	return YamlKey(string(k) + fmt.Sprintf("[%d]", idx))
}

// Segments parses the key into its component segments.
func (k YamlKey) Segments() []segment {
	_, segs, err := ParseYamlKey(string(k))
	if err != nil {
		return nil
	}
	return segs
}

// ParseYamlKey parses a dot/bracket-separated YAML path string into a
// canonical YamlKey and its segments.
func ParseYamlKey(s string) (YamlKey, []segment, error) {
	if s == "" {
		return "", nil, nil
	}
	var segs []segment
	var buf []byte
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; ch {
		case '.':
			if len(buf) > 0 {
				segs = append(segs, segment{key: string(buf)})
				buf = buf[:0]
			}
		case '[':
			if len(buf) > 0 {
				segs = append(segs, segment{key: string(buf)})
				buf = buf[:0]
			}
			j := i + 1
			for j < len(s) && s[j] != ']' {
				j++
			}
			if j >= len(s) {
				return "", nil, fmt.Errorf("unclosed bracket at position %d", i)
			}
			idx, err := strconv.Atoi(s[i+1 : j])
			if err != nil {
				return "", nil, fmt.Errorf("invalid index %q at position %d", s[i+1:j], i)
			}
			segs = append(segs, segment{key: s[i+1 : j], isIndex: true, index: idx})
			i = j
		default:
			buf = append(buf, ch)
		}
	}
	if len(buf) > 0 {
		segs = append(segs, segment{key: string(buf)})
	}
	canonical := ""
	for _, seg := range segs {
		if seg.isIndex {
			canonical += fmt.Sprintf("[%s]", seg.key)
		} else {
			if canonical != "" {
				canonical += "."
			}
			canonical += seg.key
		}
	}
	return YamlKey(canonical), segs, nil
}
