package gig

import (
	"context"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

type Mutator interface {
	Mutate(ctx context.Context, node *yaml.Node) error
}

type MutatorFunc func(ctx context.Context, node *yaml.Node) error

func (f MutatorFunc) Mutate(ctx context.Context, node *yaml.Node) error { return f(ctx, node) }

type LoadOption func(*loader) error

func Load[T any](ctx context.Context, src io.Reader, opts ...LoadOption) (T, error) {
	var zero T
	l := &loader{
		validate: true,
	}
	for _, opt := range opts {
		if err := opt(l); err != nil {
			return zero, err
		}
	}

	// Build the reader list: primary source + any WithSources
	readers := append([]io.Reader{src}, l.sources...)

	// Build mutator chain
	if !l.customMutators {
		l.mutators = buildDefaultMutators(l.fileOptions, l.envOptions)
	}

	// Process sources
	var merged *yaml.Node
	for _, r := range readers {
		data, err := io.ReadAll(r)
		if err != nil {
			return zero, fmt.Errorf("read source: %w", err)
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			continue
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return zero, fmt.Errorf("yaml: %w", err)
		}

		// Apply mutators
		for _, m := range l.mutators {
			if err := m.Mutate(ctx, &doc); err != nil {
				return zero, err
			}
		}

		if merged == nil {
			merged = &doc
		} else {
			merged = mergeNodes(merged, &doc)
		}
	}

	if merged == nil {
		return zero, nil
	}

	// Decode
	if err := merged.Decode(&zero); err != nil {
		return zero, fmt.Errorf("unmarshal: %w", err)
	}

	// Validate
	if l.validate {
		if v, ok := any(zero).(Validator); ok {
			if err := v.Validate(); err != nil {
				return zero, err
			}
		} else if v, ok := any(&zero).(Validator); ok {
			if err := v.Validate(); err != nil {
				return zero, err
			}
		}
		if v, ok := any(zero).(ValidatorContext); ok {
			if err := v.ValidateContext(ctx); err != nil {
				return zero, err
			}
		} else if v, ok := any(&zero).(ValidatorContext); ok {
			if err := v.ValidateContext(ctx); err != nil {
				return zero, err
			}
		}
	}

	return zero, nil
}

func WithMutators(m ...Mutator) LoadOption {
	return func(l *loader) error {
		l.mutators = m
		l.customMutators = true
		return nil
	}
}

func WithSources(readers ...io.Reader) LoadOption {
	return func(l *loader) error {
		l.sources = append(l.sources, readers...)
		return nil
	}
}

func WithValidation(enabled bool) LoadOption {
	return func(l *loader) error {
		l.validate = enabled
		return nil
	}
}

func WithFileOptions(opts ...FileOption) LoadOption {
	return func(l *loader) error {
		l.fileOptions = append(l.fileOptions, opts...)
		return nil
	}
}

func WithEnvOptions(opts ...EnvOption) LoadOption {
	return func(l *loader) error {
		l.envOptions = append(l.envOptions, opts...)
		return nil
	}
}

type loader struct {
	mutators       []Mutator
	sources        []io.Reader
	validate       bool
	fileOptions    []FileOption
	envOptions     []EnvOption
	customMutators bool
}

type ResolveError struct {
	Path string
	Err  error
}

func (e ResolveError) Error() string {
	if e.Err == nil {
		return e.Path
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e ResolveError) Unwrap() error { return e.Err }

type Validator interface {
	Validate() error
}

type ValidatorContext interface {
	ValidateContext(context.Context) error
}
