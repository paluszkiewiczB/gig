package gig

import (
	"context"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ErrOptionalUnset is returned by an optional tag handler when the value is
// absent. TagResolver uses this to remove the key from its parent mapping.
var ErrOptionalUnset = errors.New("optional tag value is unset")

// TagResolver walks a YAML tree and dispatches tagged scalars to registered
// Mutator handlers. Any tag without a registered handler is a resolution
// error.
type TagResolver struct {
	handlers map[string]Mutator
}

// NewTagResolver creates a TagResolver that dispatches the given tags to
// their corresponding Mutator handlers.
func NewTagResolver(handlers map[string]Mutator) *TagResolver {
	return &TagResolver{handlers: handlers}
}

// Handle registers a Mutator for the given tag and returns the TagResolver
// for chaining. If the handler map is nil, it is lazily initialized.
func (tr *TagResolver) Handle(tag string, handler Mutator) *TagResolver {
	if tr.handlers == nil {
		tr.handlers = make(map[string]Mutator)
	}
	tr.handlers[tag] = handler

	return tr
}

// Mutate walks the YAML node tree and applies registered handlers to tagged
// scalars. If a handler returns ErrOptionalUnset, the key-value pair is
// removed from its parent mapping.
func (tr *TagResolver) Mutate(ctx context.Context, node *yaml.Node) error {
	return tr.walk(ctx, node, nil, -1)
}

func (tr *TagResolver) walk(ctx context.Context, node *yaml.Node, parent *yaml.Node, index int) error {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.DocumentNode:
		return tr.walkDocument(ctx, node)
	case yaml.MappingNode:
		return tr.walkMapping(ctx, node)
	case yaml.SequenceNode:
		return tr.walkSequence(ctx, node)
	case yaml.ScalarNode:
		return tr.walkScalar(ctx, node, parent, index)
	case yaml.AliasNode:
		return nil
	}

	return nil
}

func (tr *TagResolver) walkDocument(ctx context.Context, node *yaml.Node) error {
	for i := range node.Content {
		if err := tr.walk(ctx, node.Content[i], node, i); err != nil {
			return err
		}
	}

	return nil
}

func (tr *TagResolver) walkMapping(ctx context.Context, node *yaml.Node) error {
	for pairIndex := 0; pairIndex < len(node.Content); pairIndex += 2 {
		removed, err := tr.walkMappingPair(ctx, node, pairIndex)
		if err != nil {
			return err
		}
		if removed {
			pairIndex -= 2
		}
	}

	return tr.walkMappingChildren(ctx, node)
}

func (tr *TagResolver) walkMappingPair(ctx context.Context, node *yaml.Node, pairIndex int) (bool, error) {
	keyNode := node.Content[pairIndex]
	valNode := node.Content[pairIndex+1]

	if valNode.Tag != "" && tr.isTagged(valNode) {
		if err := tr.walk(ctx, valNode, node, pairIndex+1); err != nil {
			if errors.Is(err, ErrOptionalUnset) {
				node.Content = removePair(node.Content, pairIndex)

				return true, nil
			}

			return false, err
		}

		return false, nil
	}

	if keyNode.Tag != "" && tr.isTagged(keyNode) {
		if err := tr.walk(ctx, keyNode, node, pairIndex); err != nil {
			if errors.Is(err, ErrOptionalUnset) {
				node.Content = removePair(node.Content, pairIndex)

				return true, nil
			}

			return false, err
		}

		return false, nil
	}

	return false, nil
}

func (tr *TagResolver) walkMappingChildren(ctx context.Context, node *yaml.Node) error {
	for i, child := range node.Content {
		if child.Kind != yaml.ScalarNode {
			if err := tr.walk(ctx, child, node, i); err != nil {
				return err
			}
		}
	}

	return nil
}

func (tr *TagResolver) walkSequence(ctx context.Context, node *yaml.Node) error {
	for index := 0; index < len(node.Content); index++ {
		child := node.Content[index]
		if child.Tag != "" && tr.isTagged(child) {
			if err := tr.walk(ctx, child, node, index); err != nil {
				if errors.Is(err, ErrOptionalUnset) {
					node.Content = append(node.Content[:index], node.Content[index+1:]...)
					index--

					continue
				}

				return err
			}

			continue
		}
		if child.Kind != yaml.ScalarNode {
			if err := tr.walk(ctx, child, node, index); err != nil {
				return err
			}
		}
	}

	return nil
}

func (tr *TagResolver) walkScalar(ctx context.Context, node *yaml.Node, parent *yaml.Node, index int) error {
	if !tr.isTaggedScalar(node) {
		return nil
	}

	handler, ok := tr.handlers[node.Tag]
	if !ok {
		return newResolveError(buildPath(parent, index), fmt.Errorf("unknown tag %q", node.Tag))
	}

	return tr.applyScalarMutator(ctx, node, parent, index, handler)
}

func (tr *TagResolver) isTaggedScalar(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && tr.isTagged(node)
}

func (tr *TagResolver) applyScalarMutator(
	ctx context.Context, node *yaml.Node, parent *yaml.Node, index int, handler Mutator,
) error {
	path := buildPath(parent, index)
	if err := handler.Mutate(ctx, node); err != nil {
		return newResolveError(path, fmt.Errorf("handler: %w", err))
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
