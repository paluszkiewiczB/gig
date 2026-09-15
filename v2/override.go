package gig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// NewOverride creates a Mutator that overrides specific YAML paths with
// literal string values before the configured resolvers run. It returns an
// error if any of the given keys is not a valid YAML path.
func NewOverride(overrides map[YamlKey]string) (Mutator, error) {
	entries := make([]overrideEntry, 0, len(overrides))
	errs := make([]error, 0)
	for key, value := range overrides {
		segments, err := parseSegments(string(key))
		if err != nil {
			errs = append(errs, fmt.Errorf("gig: invalid override key %q: %w", key, err))

			continue
		}
		entries = append(entries, overrideEntry{segments: segments, value: value})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return &overrideMutator{entries: entries}, nil
}

type overrideEntry struct {
	segments []segment
	value    string
}

type overrideMutator struct {
	entries []overrideEntry
}

func (m *overrideMutator) Mutate(_ context.Context, node *yaml.Node) error {
	return m.apply(node)
}

func (m *overrideMutator) apply(node *yaml.Node) error {
	target := node
	if target.Kind == yaml.DocumentNode && len(target.Content) > 0 {
		target = target.Content[0]
	}
	for _, entry := range m.entries {
		m.setValue(target, entry.segments, entry.value)
	}

	return nil
}

func (m *overrideMutator) setValue(node *yaml.Node, segments []segment, value string) {
	if len(segments) == 0 {
		if node.Kind == yaml.ScalarNode {
			node.Value = value
			node.Tag = ""
		}

		return
	}

	if node.Kind == yaml.MappingNode {
		m.setMappingValue(node, segments, value)

		return
	}

	if node.Kind == yaml.SequenceNode && segments[0].isIndex {
		m.setSequenceValue(node, segments, value)
	}
}

func (m *overrideMutator) setMappingValue(node *yaml.Node, segments []segment, value string) {
	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		if keyNode.Value != segments[0].key {
			continue
		}
		if len(segments) == 1 {
			node.Content[index+1].Kind = yaml.ScalarNode
			node.Content[index+1].Tag = ""
			node.Content[index+1].Value = value
			node.Content[index+1].Content = nil

			return
		}
		m.setValue(node.Content[index+1], segments[1:], value)

		return
	}
	m.createPath(node, segments, value)
}

func (m *overrideMutator) setSequenceValue(node *yaml.Node, segments []segment, value string) {
	index := segments[0].index
	if index >= len(node.Content) {
		return
	}
	if len(segments) == 1 {
		node.Content[index].Kind = yaml.ScalarNode
		node.Content[index].Tag = ""
		node.Content[index].Value = value
		node.Content[index].Content = nil

		return
	}
	m.setValue(node.Content[index], segments[1:], value)
}

func (m *overrideMutator) createPath(node *yaml.Node, segments []segment, value string) {
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: segments[0].key}
	if len(segments) == 1 {
		valueNode := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
		node.Content = append(node.Content, keyNode, valueNode)

		return
	}

	valueNode := &yaml.Node{Kind: yaml.MappingNode}
	node.Content = append(node.Content, keyNode, valueNode)
	m.setValue(valueNode, segments[1:], value)
}

// EnvOverrides reads environment variables with the given prefix and returns
// a map of YamlKey to string values. The prefix is stripped, and __ becomes
// the separator between path segments, while _ becomes the separator between
// keys. For example, with prefix "CFG_", CFG_database__host maps to
// database.host.
func EnvOverrides(prefix string) map[YamlKey]string {
	result := make(map[YamlKey]string)
	for _, env := range os.Environ() {
		name, val, _ := strings.Cut(env, "=")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		trimmed := strings.TrimPrefix(name, prefix)
		if trimmed == "" {
			continue
		}
		key := envKeyToYamlKey(trimmed)
		result[key] = val
	}

	return result
}

func envKeyToYamlKey(raw string) YamlKey {
	var result YamlKey
	current := ""
	for index := 0; index < len(raw); index++ {
		char := raw[index]
		switch {
		case char == '_' && index+1 < len(raw) && raw[index+1] == '_':
			result = result.Key(current)
			result = result.Key("")
			current = ""
			index++
		case char == '_':
			if current != "" {
				result = result.Key(current)
			}
			current = ""
		default:
			current += string(char)
		}
	}
	if current != "" {
		result = result.Key(current)
	}

	return result
}
