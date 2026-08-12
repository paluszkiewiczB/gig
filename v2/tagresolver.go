package gig

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var ErrOptionalUnset = fmt.Errorf("optional tag value is unset")

type TagResolver struct {
	handlers map[string]Mutator
}

func NewTagResolver(handlers map[string]Mutator) *TagResolver {
	return &TagResolver{handlers: handlers}
}

func (tr *TagResolver) Handle(tag string, handler Mutator) *TagResolver {
	if tr.handlers == nil {
		tr.handlers = make(map[string]Mutator)
	}
	tr.handlers[tag] = handler
	return tr
}

func (tr *TagResolver) Mutate(ctx context.Context, node *yaml.Node) error {
	return tr.walk(ctx, node, nil, -1)
}

func (tr *TagResolver) walk(ctx context.Context, node *yaml.Node, parent *yaml.Node, index int) error { //nolint:gocognit,cyclop
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.DocumentNode:
		for i := range node.Content {
			if err := tr.walk(ctx, node.Content[i], node, i); err != nil {
				return err
			}
		}

	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]

			if valNode.Tag != "" && tr.isTagged(valNode) {
				if err := tr.walk(ctx, valNode, node, i+1); err != nil {
					if errors.Is(err, ErrOptionalUnset) {
						node.Content = removePair(node.Content, i)
						i -= 2
						continue
					}
					return err
				}
				continue
			}

			if keyNode.Tag != "" && tr.isTagged(keyNode) {
				if err := tr.walk(ctx, keyNode, node, i); err != nil {
					if errors.Is(err, ErrOptionalUnset) {
						node.Content = removePair(node.Content, i)
						i -= 2
						continue
					}
					return err
				}
				continue
			}
		}

		// Walk children
		for i, child := range node.Content {
			if child.Kind != yaml.ScalarNode {
				if err := tr.walk(ctx, child, node, i); err != nil {
					return err
				}
			}
		}

	case yaml.SequenceNode:
		for i := 0; i < len(node.Content); i++ {
			child := node.Content[i]
			if child.Tag != "" && tr.isTagged(child) {
				if err := tr.walk(ctx, child, node, i); err != nil {
					if errors.Is(err, ErrOptionalUnset) {
						node.Content = append(node.Content[:i], node.Content[i+1:]...)
						i--
						continue
					}
					return err
				}
				continue
			}
			if child.Kind != yaml.ScalarNode {
				if err := tr.walk(ctx, child, node, i); err != nil {
					return err
				}
			}
		}
	}

	// Handle scalar tagged nodes (leaf)
	if node.Kind == yaml.ScalarNode && node.Tag != "" && node.Tag != "!!str" && node.Tag != "!!int" && node.Tag != "!!bool" && node.Tag != "!!float" && node.Tag != "!!null" {
		handler, ok := tr.handlers[node.Tag]
		if !ok {
			if strings.HasSuffix(node.Tag, "?") {
				node.Tag = ""
				return nil
			}
			// Unknown tag - strip it so it decodes as literal
			node.Tag = ""
			node.Style = 0
			return nil
		}

		path := buildPath(parent, index)
		if err := handler.Mutate(ctx, node); err != nil {
			if errors.Is(err, ErrOptionalUnset) {
				return err
			}
			return newResolveError(path, err)
		}
	}

	return nil
}

func (tr *TagResolver) isTagged(node *yaml.Node) bool {
	return node.Tag != "" && node.Tag != "!!str" && node.Tag != "!!int" &&
		node.Tag != "!!bool" && node.Tag != "!!float" && node.Tag != "!!null" &&
		node.Tag != "!!map" && node.Tag != "!!seq"
}

func newResolveError(path string, err error) *ResolveError {
	return &ResolveError{Path: path, Err: err}
}

func removePair(content []*yaml.Node, keyIndex int) []*yaml.Node {
	return append(content[:keyIndex], content[keyIndex+2:]...)
}
