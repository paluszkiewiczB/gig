package gig

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func mergeNodes(dst, src *yaml.Node) *yaml.Node {
	dstNode := dst
	if dst.Kind == yaml.DocumentNode && len(dst.Content) > 0 {
		dstNode = dst.Content[0]
	}
	srcNode := src
	if src.Kind == yaml.DocumentNode && len(src.Content) > 0 {
		srcNode = src.Content[0]
	}
	return mergeNode(dstNode, srcNode)
}

func mergeNode(dst, src *yaml.Node) *yaml.Node {
	switch dst.Kind {
	case yaml.MappingNode:
		if src.Kind != yaml.MappingNode {
			return src
		}
		return mergeMapping(dst, src)
	default:
		return src
	}
}

func mergeMapping(dst, src *yaml.Node) *yaml.Node {
	srcKeys := make(map[string]int)
	for i := 0; i < len(src.Content); i += 2 {
		srcKeys[src.Content[i].Value] = i
	}

	result := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	result.Content = make([]*yaml.Node, 0, len(dst.Content)+len(src.Content))
	added := make(map[string]bool)

	for i := 0; i < len(src.Content); i += 2 {
		key := src.Content[i].Value
		result.Content = append(result.Content, cloneNode(src.Content[i]), cloneNode(src.Content[i+1]))
		added[key] = true
	}

	for i := 0; i < len(dst.Content); i += 2 {
		key := dst.Content[i].Value
		if !added[key] {
			result.Content = append(result.Content, cloneNode(dst.Content[i]), cloneNode(dst.Content[i+1]))
		}
	}

	return result
}

func cloneNode(n *yaml.Node) *yaml.Node {
	c := &yaml.Node{
		Kind:        n.Kind,
		Tag:         n.Tag,
		Value:       n.Value,
		Style:       n.Style,
		Anchor:      n.Anchor,
		Alias:       n.Alias,
		Line:        n.Line,
		Column:      n.Column,
		HeadComment: n.HeadComment,
		LineComment: n.LineComment,
		FootComment: n.FootComment,
	}
	for _, child := range n.Content {
		c.Content = append(c.Content, cloneNode(child))
	}
	return c
}

func buildPath(parent *yaml.Node, index int) string {
	if parent == nil {
		return "$"
	}

	switch parent.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(parent.Content); i += 2 {
			if i+1 < len(parent.Content) && i+1 == index {
				return "$." + parent.Content[i].Value
			}
		}
	case yaml.SequenceNode:
		for i := 0; i < len(parent.Content); i++ {
			if i == index {
				return fmt.Sprintf("$[%d]", i)
			}
		}
	case yaml.DocumentNode:
		return "$"
	}

	return "$"
}
