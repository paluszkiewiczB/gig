package gig

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mutator processes a YAML node and mutates it in place.
// Implementations should set the resolved value and clear the custom YAML tag.
type Mutator interface {
	Mutate(ctx context.Context, node *yaml.Node) error
}

// MutatorFunc is an adapter to use a plain function as a Mutator.
type MutatorFunc func(ctx context.Context, node *yaml.Node) error

// Mutate calls f(ctx, node).
func (f MutatorFunc) Mutate(ctx context.Context, node *yaml.Node) error { return f(ctx, node) }

// LoadOption configures a call to Load.
type LoadOption func(*loader) error

// Load reads YAML from src, applies opts, runs the mutator chain, decodes the
// result into T, and optionally validates it.
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
		var err error
		l.mutators, err = buildDefaultMutators(l.fileOptions, l.envOptions)
		if err != nil {
			return zero, err
		}
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
				return zero, fmt.Errorf("mutate: %w", err)
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
				return zero, fmt.Errorf("validate: %w", err)
			}
		} else if v, ok := any(&zero).(Validator); ok {
			if err := v.Validate(); err != nil {
				return zero, fmt.Errorf("validate: %w", err)
			}
		}
		if v, ok := any(zero).(ValidatorContext); ok {
			if err := v.ValidateContext(ctx); err != nil {
				return zero, fmt.Errorf("validate: %w", err)
			}
		} else if v, ok := any(&zero).(ValidatorContext); ok {
			if err := v.ValidateContext(ctx); err != nil {
				return zero, fmt.Errorf("validate: %w", err)
			}
		}
	}

	return zero, nil
}

func isNil[T any](v T) bool { return any(v) == nil || reflect.ValueOf(v).IsNil() }

// WithMutators sets the mutator chain. When omitted, Load uses DefaultMutators.
func WithMutators(m ...Mutator) LoadOption {
	return func(l *loader) error {
		if i := slices.IndexFunc(m, isNil[Mutator]); i != -1 {
			return fmt.Errorf("gig: nil mutator at index %d", i)
		}
		l.mutators = m
		l.customMutators = true
		return nil
	}
}

// WithSources adds YAML readers that override the primary source in order.
// Mapping values are merged recursively; scalar and sequence values replace
// earlier values.
func WithSources(readers ...io.Reader) LoadOption {
	return func(l *loader) error {
		if i := slices.IndexFunc(readers, isNil[io.Reader]); i != -1 {
			return fmt.Errorf("gig: nil source reader at index %d", i)
		}
		l.sources = append(l.sources, readers...)
		return nil
	}
}

// WithValidation enables or disables post-unmarshal validation. Validation is
// enabled by default.
func WithValidation(enabled bool) LoadOption {
	return func(l *loader) error {
		l.validate = enabled
		return nil
	}
}

// WithFileOptions configures the default file handler. Ignored when custom
// mutators are provided via WithMutators.
func WithFileOptions(opts ...FileOption) LoadOption {
	return func(l *loader) error {
		if i := slices.IndexFunc(opts, isNil[FileOption]); i != -1 {
			return fmt.Errorf("gig: nil file option at index %d", i)
		}
		l.fileOptions = append(l.fileOptions, opts...)
		return nil
	}
}

// WithEnvOptions configures the default environment handler. Ignored when custom
// mutators are provided via WithMutators.
func WithEnvOptions(opts ...EnvOption) LoadOption {
	return func(l *loader) error {
		if i := slices.IndexFunc(opts, isNil[EnvOption]); i != -1 {
			return fmt.Errorf("gig: nil env option at index %d", i)
		}
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

// ResolveError reports a failure while resolving a YAML value.
type ResolveError struct {
	// Path is a gig configuration path rooted at $, such as $.database.host or
	// $.servers[0].host.
	Path string
	// Err is the underlying resolution failure.
	Err error
}

// Error returns the configuration path and resolution failure.
func (e ResolveError) Error() string {
	if e.Err == nil {
		return e.Path
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying resolution failure.
func (e ResolveError) Unwrap() error { return e.Err }

// Validator validates a configuration value after it has been unmarshaled.
type Validator interface {
	Validate() error
}

// ValidatorContext validates a configuration value with the loading context.
type ValidatorContext interface {
	ValidateContext(context.Context) error
}
