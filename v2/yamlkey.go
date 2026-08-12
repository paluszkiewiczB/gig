package gig

import "fmt"

type segment struct {
	key     string
	isIndex bool
}

type YamlKey string

func (k YamlKey) Key(name string) YamlKey {
	if k == "" {
		return YamlKey(name)
	}
	return YamlKey(string(k) + "." + name)
}

func (k YamlKey) Index(idx int) YamlKey {
	return YamlKey(string(k) + fmt.Sprintf("[%d]", idx))
}

func (k YamlKey) Segments() []segment {
	_, segs, _ := ParseYamlKey(string(k))
	return segs
}

func ParseYamlKey(s string) (YamlKey, []segment, error) {
	if s == "" {
		return "", nil, nil
	}
	var segs []segment
	var buf []byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '.':
			if len(buf) > 0 {
				segs = append(segs, segment{key: string(buf)})
				buf = buf[:0]
			}
		case ch == '[':
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
			segs = append(segs, segment{key: s[i+1 : j], isIndex: true})
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
