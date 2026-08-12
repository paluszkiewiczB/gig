package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────────────────────────────────────
// Core types
// ──────────────────────────────────────────────────────────

// Mutator mutates a per-source YAML node tree before merge.
type Mutator interface {
	Mutate(ctx context.Context, node *yaml.Node) error
}

// MutatorFunc adapts a function to Mutator.
type MutatorFunc func(ctx context.Context, node *yaml.Node) error

func (f MutatorFunc) Mutate(ctx context.Context, node *yaml.Node) error { return f(ctx, node) }

// LoadOption configures a call to Load.
type LoadOption func(*loader) error

// ──────────────────────────────────────────────────────────
// YamlKey — structured YAML path, comparable (map-key safe)
// ──────────────────────────────────────────────────────────

// segment is one step in a YAML path.
type segment struct {
	key     string
	isIndex bool // true = sequence index, false = mapping key
}

// YamlKey is a structured YAML mapping path.
// Comparable — safe to use as a map key.
// Construct with builder methods or ParseYamlKey.
type YamlKey string

// Key appends a mapping key segment.
func (k YamlKey) Key(name string) YamlKey {
	if k == "" {
		return YamlKey(name)
	}
	return YamlKey(string(k) + "." + name)
}

// Index appends a sequence index segment.
func (k YamlKey) Index(idx int) YamlKey {
	return YamlKey(string(k) + fmt.Sprintf("[%d]", idx))
}

// Segments lazily parses and returns the path segments.
func (k YamlKey) Segments() []segment {
	s, _, _ := ParseYamlKey(string(k))
	_ = s
	return nil
}

// ParseYamlKey parses a YAML path string into its canonical key
// and pre-computed segments. The key is safe to use as a map key.
func ParseYamlKey(s string) (YamlKey, []segment, error) {
	_ = s
	return "", nil, nil
}

// ErrOptionalUnset is returned by a tag handler to signal that the value
// is absent and the field should be removed.
var ErrOptionalUnset = errors.New("optional tag value is unset")

// ──────────────────────────────────────────────────────────
// Load
// ──────────────────────────────────────────────────────────

func Load[T any](ctx context.Context, src io.Reader, opts ...LoadOption) (T, error) {
	var zero T
	_ = opts
	return zero, nil
}

// WithMutators replaces the entire mutator chain.
// If used, WithFileOptions and WithEnvOptions are ignored.
func WithMutators(m ...Mutator) LoadOption {
	_ = m
	return func(l *loader) error { return nil }
}

func WithSources(readers ...io.Reader) LoadOption {
	_ = readers
	return func(l *loader) error { return nil }
}

func WithValidation(enabled bool) LoadOption {
	_ = enabled
	return func(l *loader) error { return nil }
}

// WithFileOptions configures the default !file / !file? handler.
// Only applies when the default mutator chain is used (no WithMutators).
func WithFileOptions(opts ...FileOption) LoadOption {
	_ = opts
	return func(l *loader) error { return nil }
}

// WithEnvOptions configures the default !env / !env? handler.
// Only applies when the default mutator chain is used (no WithMutators).
func WithEnvOptions(opts ...EnvOption) LoadOption {
	_ = opts
	return func(l *loader) error { return nil }
}

// ──────────────────────────────────────────────────────────
// TagResolver — single mutator for all tag dispatching
// ──────────────────────────────────────────────────────────

type TagResolver struct {
	handlers map[string]Mutator
}

func NewTagResolver(handlers map[string]Mutator) *TagResolver {
	return &TagResolver{handlers: handlers}
}

func (tr *TagResolver) Handle(tag string, handler Mutator) *TagResolver {
	tr.handlers[tag] = handler
	return tr
}

func (tr *TagResolver) Mutate(ctx context.Context, node *yaml.Node) error {
	_ = ctx
	_ = node
	return nil
}

// ──────────────────────────────────────────────────────────
// Handler factories (return Mutator, for use inside NewTagResolver)
// ──────────────────────────────────────────────────────────

type EnvLookup func(name string) (value string, set bool)

type EnvExpander func(expression string, optional bool) (value string, present bool, err error)

type EnvOption func(*envConfig)

type envConfig struct {
	lookup   EnvLookup
	expander EnvExpander
}

func WithEnvLookup(lookup EnvLookup) EnvOption {
	return func(cfg *envConfig) {
		cfg.lookup = lookup
	}
}

func WithEnvExpander(expander EnvExpander) EnvOption {
	return func(cfg *envConfig) {
		cfg.expander = expander
	}
}

func NewEnvHandler(opts ...EnvOption) Mutator {
	return MutatorFunc(func(_ context.Context, node *yaml.Node) error {
		_ = opts
		_ = node
		return nil
	})
}

func DefaultEnvHandler() Mutator { return NewEnvHandler() }

type FileOption func(*fileConfig)

type fileConfig struct {
	baseDir string
	fsys    fs.FS
	root    *os.Root
}

func WithBaseDir(dir string) FileOption {
	return func(cfg *fileConfig) {
		cfg.baseDir = dir
	}
}

func WithFS(fsys fs.FS) FileOption {
	return func(cfg *fileConfig) {
		cfg.fsys = fsys
	}
}

func WithRoot(root *os.Root) FileOption {
	return func(cfg *fileConfig) {
		cfg.root = root
	}
}

func NewFileHandler(opts ...FileOption) Mutator {
	return MutatorFunc(func(_ context.Context, node *yaml.Node) error {
		_ = opts
		_ = node
		return nil
	})
}

func DefaultFileHandler() Mutator { return NewFileHandler() }

// ──────────────────────────────────────────────────────────
// Env override (first-class mutator)
// ──────────────────────────────────────────────────────────

func NewOverride(overrides map[YamlKey]string) Mutator {
	return MutatorFunc(func(_ context.Context, node *yaml.Node) error {
		_ = overrides
		_ = node
		return nil
	})
}

// EnvOverrides reads os.Environ() and returns pre-parsed YamlKey → value map.
func EnvOverrides(prefix string) map[YamlKey]string {
	_ = prefix
	return nil
}

// ──────────────────────────────────────────────────────────
// Defaults
// ──────────────────────────────────────────────────────────

func DefaultMutators() []Mutator {
	return []Mutator{
		NewTagResolver(map[string]Mutator{
			"!env":   DefaultEnvHandler(),
			"!env?":  DefaultEnvHandler(),
			"!file":  DefaultFileHandler(),
			"!file?": DefaultFileHandler(),
		}),
	}
}

// ──────────────────────────────────────────────────────────
// Error / Validation
// ──────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────
// Internal
// ──────────────────────────────────────────────────────────

type loader struct {
	mutators    []Mutator
	overrides   []io.Reader
	validate    bool
	fileOptions []FileOption
	envOptions  []EnvOption
}

// ──────────────────────────────────────────────────────────
// Demonstration of every possible use-case
// ──────────────────────────────────────────────────────────

type Config struct {
	Name    string `yaml:"name"`
	Port    int    `yaml:"port"`
	Enabled bool   `yaml:"enabled"`
	Nested  struct {
		Value string `yaml:"value"`
	} `yaml:"nested"`
}

func main() {
	ctx := context.Background()

	// ─── 1. Default — just works ──────────────────────────
	Load[Config](ctx, someReader())

	// ─── 2. Custom env lookup ─────────────────────────────
	Load[Config](ctx, someReader(), WithMutators(
		NewTagResolver(map[string]Mutator{
			"!env": NewEnvHandler(WithEnvLookup(func(name string) (string, bool) {
				return "", false
			})),
			"!env?": NewEnvHandler(WithEnvLookup(func(name string) (string, bool) {
				return "", false
			})),
			"!file":  DefaultFileHandler(),
			"!file?": DefaultFileHandler(),
		}),
	))

	// ─── 3. Custom file base dir via WithFileOptions ──────
	Load[Config](ctx, someReader(),
		WithFileOptions(WithBaseDir("/etc/myapp")),
		WithEnvOptions(WithEnvLookup(os.LookupEnv)),
	)

	// ─── 4. Env override (GIG_ prefix) ────────────────────
	Load[Config](ctx, someReader(), WithMutators(
		append(
			DefaultMutators(),
			NewOverride(EnvOverrides("GIG_")),
		)...,
	))

	// ─── 5. Env override BEFORE env expressions (reorder) ─
	Load[Config](ctx, someReader(), WithMutators(
		NewOverride(EnvOverrides("GIG_")),
		NewTagResolver(map[string]Mutator{
			"!env":   DefaultEnvHandler(),
			"!env?":  DefaultEnvHandler(),
			"!file":  DefaultFileHandler(),
			"!file?": DefaultFileHandler(),
		}),
	))

	// ─── 6. Custom vault resolver only (replace all) ─────
	vaultHandler := MutatorFunc(func(_ context.Context, node *yaml.Node) error {
		_ = node
		return nil
	})
	vaultOptionalHandler := MutatorFunc(func(_ context.Context, node *yaml.Node) error {
		_ = node
		return ErrOptionalUnset
	})
	Load[Config](ctx, someReader(), WithMutators(
		NewTagResolver(map[string]Mutator{
			"!vault":  vaultHandler,
			"!vault?": vaultOptionalHandler,
		}),
	))

	// ─── 7. Testing (no global dependency) ────────────────
	Load[Config](ctx, someReader(), WithMutators(
		NewOverride(map[YamlKey]string{
			YamlKey("").Key("name"):                "test-value",
			YamlKey("").Key("nested").Key("value"): "nested-test",
		}),
	))

	// ─── 8. Anonymous inline mutator ──────────────────────
	Load[Config](ctx, someReader(), WithMutators(
		append(DefaultMutators(),
			MutatorFunc(func(_ context.Context, node *yaml.Node) error {
				_ = node
				return nil
			}),
		)...,
	))

	// ─── 9. Combined: env override + vault + custom file ─
	Load[Config](ctx, someReader(), WithMutators(
		NewOverride(EnvOverrides("MYAPP_")),
		NewTagResolver(map[string]Mutator{
			"!env":    DefaultEnvHandler(),
			"!env?":   DefaultEnvHandler(),
			"!file":   NewFileHandler(WithBaseDir("/secrets")),
			"!file?":  NewFileHandler(WithBaseDir("/secrets")),
			"!vault":  vaultHandler,
			"!vault?": vaultOptionalHandler,
		}),
	))

	// ─── 10. With sources + validation disabled ──────────
	Load[Config](ctx, someReader(),
		WithSources(someReader(), someReader()),
		WithValidation(false),
	)

	// ─── 11. Full custom chain, no defaults at all ───────
	Load[Config](ctx, someReader(), WithMutators(
		NewOverride(map[YamlKey]string{
			YamlKey("").Key("port"): "8080",
		}),
		NewTagResolver(map[string]Mutator{
			"!env":    DefaultEnvHandler(),
			"!env?":   DefaultEnvHandler(),
			"!file":   NewFileHandler(WithRoot(mustOpenRoot("/etc"))),
			"!file?":  NewFileHandler(WithRoot(mustOpenRoot("/etc"))),
			"!token":  NewEnvHandler(WithEnvLookup(os.LookupEnv)),
			"!token?": NewEnvHandler(WithEnvLookup(os.LookupEnv)),
		}),
		MutatorFunc(func(_ context.Context, node *yaml.Node) error {
			_ = node
			return nil
		}),
	))

	// ─── 12. Bracket notation for dotted keys ────────────
	Load[Config](ctx, someReader(), WithMutators(
		NewOverride(map[YamlKey]string{
			YamlKey("foo").Index(0).Key("a.b"):        "seq-first-dot-key",
			YamlKey("foo").Key("0").Key("a").Key("b"): "map-zero-nested",
		}),
	))
}

func someReader() io.Reader            { return nil }
func mustOpenRoot(dir string) *os.Root { r, _ := os.OpenRoot(dir); return r }
