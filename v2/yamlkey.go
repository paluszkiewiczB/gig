package gig

import (
	"fmt"
	"strconv"
	"strings"
)

type segment struct {
	key     string
	isIndex bool
	index   int
}

// YamlKey is a canonical dot-separated path to a YAML node, such as
// "database.host" or "servers[0].host".
//
// Build paths with Key and Index. Each Key argument is treated as one literal
// segment, so field names containing '.' or '[' are supported; those
// characters, and a backslash, are escaped with a backslash in the string
// form.
type YamlKey string

// Key returns a new YamlKey with the field name appended. The name is one
// literal segment, even if it contains '.' or '['.
func (k YamlKey) Key(name string) YamlKey {
	escaped := escapeSegment(name)
	if k == "" {
		return YamlKey(escaped)
	}

	return YamlKey(string(k) + "." + escaped)
}

// Index returns a new YamlKey with the sequence index appended.
func (k YamlKey) Index(idx int) YamlKey {
	return YamlKey(string(k) + fmt.Sprintf("[%d]", idx))
}

func escapeSegment(name string) string {
	if !strings.ContainsAny(name, `\.[`) {
		return name
	}

	var builder strings.Builder
	builder.Grow(len(name) + 4)
	for index := range len(name) {
		char := name[index]
		if char == '\\' || char == '.' || char == '[' {
			builder.WriteByte('\\')
		}
		builder.WriteByte(char)
	}

	return builder.String()
}

func parseSegments(path string) ([]segment, error) {
	segments := make([]segment, 0)
	buffer := make([]byte, 0)
	escaped := false
	for position := 0; position < len(path); position++ {
		char := path[position]
		if escaped {
			buffer = append(buffer, char)
			escaped = false

			continue
		}
		switch char {
		case '\\':
			escaped = true
		case '.':
			segments = appendKeySegment(segments, &buffer)
		case '[':
			segments = appendKeySegment(segments, &buffer)

			indexSegment, nextPosition, err := parseIndexSegment(path, position)
			if err != nil {
				return nil, err
			}
			segments = append(segments, indexSegment)
			position = nextPosition
		default:
			buffer = append(buffer, char)
		}
	}
	if escaped {
		return nil, fmt.Errorf("trailing escape in key %q", path)
	}
	if len(buffer) > 0 {
		segments = append(segments, segment{key: string(buffer), isIndex: false, index: 0})
	}

	return segments, nil
}

func appendKeySegment(segments []segment, buffer *[]byte) []segment {
	if len(*buffer) == 0 {
		return segments
	}
	segments = append(segments, segment{key: string(*buffer), isIndex: false, index: 0})
	*buffer = (*buffer)[:0]

	return segments
}

func parseIndexSegment(path string, position int) (segment, int, error) {
	closeIndex := position + 1
	for closeIndex < len(path) && path[closeIndex] != ']' {
		closeIndex++
	}
	if closeIndex >= len(path) {
		return segment{key: "", isIndex: false, index: 0}, position, fmt.Errorf("unclosed bracket at position %d", position)
	}

	index, err := strconv.Atoi(path[position+1 : closeIndex])
	if err != nil {
		return segment{key: "", isIndex: false, index: 0}, position,
			fmt.Errorf("invalid index %q at position %d", path[position+1:closeIndex], position)
	}

	return segment{key: path[position+1 : closeIndex], isIndex: true, index: index}, closeIndex, nil
}
