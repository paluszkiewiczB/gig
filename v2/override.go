package gig

import (
	"context"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// NewOverride creates a Mutator that overrides specific YAML paths with
// literal string values before the configured resolvers run.
func NewOverride(overrides map[YamlKey]string) Mutator {
	return &overrideMutator{overrides: overrides}
}

type overrideMutator struct {
	overrides map[YamlKey]string
}

func (m *overrideMutator) Mutate(ctx context.Context, node *yaml.Node) error {
	return m.apply(ctx, node)
}

func (m *overrideMutator) apply(ctx context.Context, node *yaml.Node) error {
	target := node
	if target.Kind == yaml.DocumentNode && len(target.Content) > 0 {
		target = target.Content[0]
	}
	for key, value := range m.overrides {
		m.setValue(target, key.Segments(), value)
	}
	return nil
}

func (m *overrideMutator) setValue(node *yaml.Node, segs []segment, value string) {
	if len(segs) == 0 {
		if node.Kind == yaml.ScalarNode {
			node.Value = value
			node.Tag = ""
		}
		return
	}

	if node.Kind == yaml.MappingNode {
		m.setMappingValue(node, segs, value)
		return
	}

	if node.Kind == yaml.SequenceNode && segs[0].isIndex {
		m.setSequenceValue(node, segs, value)
	}
}

func (m *overrideMutator) setMappingValue(node *yaml.Node, segs []segment, value string) {
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		if keyNode.Value == segs[0].key {
			if len(segs) == 1 {
				node.Content[i+1].Kind = yaml.ScalarNode
				node.Content[i+1].Tag = ""
				node.Content[i+1].Value = value
				node.Content[i+1].Content = nil
				return
			}
			m.setValue(node.Content[i+1], segs[1:], value)
			return
		}
	}
	m.createPath(node, segs, value)
}

func (m *overrideMutator) setSequenceValue(node *yaml.Node, segs []segment, value string) {
	idx := segs[0].index
	if idx >= len(node.Content) {
		return
	}
	if len(segs) == 1 {
		node.Content[idx].Kind = yaml.ScalarNode
		node.Content[idx].Tag = ""
		node.Content[idx].Value = value
		node.Content[idx].Content = nil
		return
	}
	m.setValue(node.Content[idx], segs[1:], value)
}

func (m *overrideMutator) createPath(node *yaml.Node, segs []segment, value string) {
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: segs[0].key}
	if len(segs) == 1 {
		valNode := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
		node.Content = append(node.Content, keyNode, valNode)
		return
	}

	valNode := &yaml.Node{Kind: yaml.MappingNode}
	node.Content = append(node.Content, keyNode, valNode)
	m.setValue(valNode, segs[1:], value)
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

func envKeyToYamlKey(s string) YamlKey {
	var result YamlKey
	current := ""
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '_' && i+1 < len(s) && s[i+1] == '_':
			result = result.Key(current)
			result = result.Key("")
			current = ""
			i++
		case ch == '_':
			if current != "" {
				result = result.Key(current)
			}
			current = ""
		default:
			current += string(ch)
		}
	}
	if current != "" {
		result = result.Key(current)
	}
	return result
}
