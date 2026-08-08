package gig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Validator validates a configuration value after it has been unmarshaled.
type Validator interface {
	Validate() error
}

// ValidatorContext validates a configuration value with the loading context.
type ValidatorContext interface {
	ValidateContext(context.Context) error
}

// Resolver resolves a tagged YAML scalar by mutating its node. A resolver
// should set the resolved value and clear the custom YAML tag.
type Resolver func(ctx context.Context, node *yaml.Node) error

// EnvLookup looks up an environment variable. The boolean reports whether the
// variable is set, allowing callers to distinguish unset from empty.
type EnvLookup func(name string) (value string, set bool)

// EnvExpander resolves an environment expression. The optional argument is
// true for !env? and false for !env. When the expression is unset and
// optional is true, return ("", false, nil) to signal an absent value
// without producing an error.
type EnvExpander func(expression string, optional bool) (value string, present bool, err error)

// Option configures a call to Load.
type Option func(*loader) error

type loader struct {
	baseDir    string
	baseDirSet bool
	envLookup  EnvLookup
	envExpand  EnvExpander
	fileSystem fs.FS
	systemFS   bool
	overrides  []io.Reader
	resolvers  map[string]Resolver
	ctx        context.Context
	validate   bool
}

// WithBaseDir sets the directory used to resolve relative !file paths.
func WithBaseDir(dir string) Option {
	return func(ld *loader) error {
		ld.baseDir = dir
		ld.baseDirSet = true
		return nil
	}
}

// WithEnvLookup sets the lookup used by the default environment expander and
// by environment expansions inside !file and !file?.
func WithEnvLookup(lookup EnvLookup) Option {
	return func(ld *loader) error {
		if lookup == nil {
			return errors.New("environment lookup must not be nil")
		}
		ld.envLookup = lookup
		return nil
	}
}

// WithEnvExpander replaces the default Bash-like environment expression
// expander used by !env and !env?.
func WithEnvExpander(expander EnvExpander) Option {
	return func(ld *loader) error {
		if expander == nil {
			return errors.New("environment expander must not be nil")
		}
		ld.envExpand = expander
		return nil
	}
}

// WithFS sets the filesystem used to resolve !file paths. When omitted,
// Load uses the unrestricted system filesystem rooted at /.
//
// Relative paths are resolved from WithBaseDir, which defaults to "."
// when a custom filesystem is configured.
//
// If both WithFS and WithRoot are provided, the last one wins.
func WithFS(fileSystem fs.FS) Option {
	return func(ld *loader) error {
		if fileSystem == nil {
			return errors.New("filesystem must not be nil")
		}
		setFileSystem(ld, fileSystem)
		return nil
	}
}

// WithRoot sets an os.Root as the filesystem used to resolve !file paths.
// Create the root with os.OpenRoot to restrict access to a directory.
func WithRoot(root *os.Root) Option {
	return func(ld *loader) error {
		if root == nil {
			return errors.New("root must not be nil")
		}
		setFileSystem(ld, rootFileSystem{root: root})
		return nil
	}
}

func setFileSystem(ld *loader, fileSystem fs.FS) {
	ld.fileSystem = fileSystem
	ld.systemFS = false
	if !ld.baseDirSet {
		ld.baseDir = "."
	}
}

type rootFileSystem struct {
	root *os.Root
}

func (r rootFileSystem) Open(name string) (fs.File, error) {
	return r.root.Open(name)
}

// WithSources adds YAML readers that override the primary source in order.
// Mapping values are merged recursively; scalar and sequence values replace
// earlier values.
func WithSources(readers ...io.Reader) Option {
	return func(ld *loader) error {
		ld.overrides = append(ld.overrides, readers...)
		return nil
	}
}

// WithResolver registers a resolver for a YAML tag, such as "!vault". To
// support optional resolution (the "?" suffix, as in "!vault?"), register
// the tag with the "?" included: WithResolver("!vault?", resolver).
func WithResolver(tag string, resolver Resolver) Option {
	return func(ld *loader) error {
		ld.resolvers[tag] = resolver
		return nil
	}
}

// WithContext sets the context passed to context-aware resolvers and
// validators. The default is context.Background().
func WithContext(ctx context.Context) Option {
	return func(ld *loader) error {
		ld.ctx = ctx
		return nil
	}
}

// WithValidation enables or disables post-unmarshal validation. Validation is
// enabled by default.
func WithValidation(enabled bool) Option {
	return func(ld *loader) error {
		ld.validate = enabled
		return nil
	}
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
func (e ResolveError) Unwrap() error {
	return e.Err
}

// Load reads YAML from src, applies opts, resolves dynamic values, decodes the
// result into T, and optionally validates it.
//
// The default base directory is the current directory. The default loading
// context is context.Background(), and validation is enabled by default.
func Load[T any](src io.Reader, opts ...Option) (T, error) {
	var zero T
	ld := loader{
		baseDir:    currentDirectory(),
		envLookup:  EnvLookup(os.LookupEnv),
		fileSystem: os.DirFS("/"),
		systemFS:   true,
		resolvers:  map[string]Resolver{},
		ctx:        context.Background(),
		validate:   true,
	}
	for _, opt := range opts {
		if err := opt(&ld); err != nil {
			return zero, err
		}
	}

	if ld.envExpand == nil {
		ld.envExpand = func(expression string, optional bool) (string, bool, error) {
			return expandEnv(expression, optional, ld.envLookup)
		}
	}
	if _, ok := ld.resolvers["!env?"]; !ok {
		ld.resolvers["!env?"] = envTagResolver(ld.envExpand)
	}
	if _, ok := ld.resolvers["!env"]; !ok {
		ld.resolvers["!env"] = envTagResolver(ld.envExpand)
	}
	if _, ok := ld.resolvers["!file"]; !ok {
		ld.resolvers["!file"] = fileResolver(ld, false)
	}
	if _, ok := ld.resolvers["!file?"]; !ok {
		ld.resolvers["!file?"] = fileResolver(ld, true)
	}

	readers := []io.Reader{src}
	readers = append(readers, ld.overrides...)

	var merged *yaml.Node
	for _, r := range readers {
		data, err := io.ReadAll(r)
		if err != nil {
			return zero, fmt.Errorf("read: %w", err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return zero, fmt.Errorf("yaml: %w", err)
		}
		if _, err := resolveOptionalNode("$", &doc, ld); err != nil {
			return zero, err
		}
		if merged == nil {
			merged = &doc
		} else {
			merged = mergeNodes(merged, &doc)
		}
	}

	if err := resolveNode("$", merged, ld); err != nil {
		return zero, err
	}

	var val T
	if err := merged.Decode(&val); err != nil {
		return zero, fmt.Errorf("unmarshal: %w", err)
	}

	if ld.validate {
		if v, ok := any(val).(Validator); ok {
			if err := v.Validate(); err != nil {
				return zero, err
			}
		} else if v, ok := any(&val).(Validator); ok {
			if err := v.Validate(); err != nil {
				return zero, err
			}
		}

		if v, ok := any(val).(ValidatorContext); ok {
			if err := v.ValidateContext(ld.ctx); err != nil {
				return zero, err
			}
		} else if v, ok := any(&val).(ValidatorContext); ok {
			if err := v.ValidateContext(ld.ctx); err != nil {
				return zero, err
			}
		}
	}

	return val, nil
}

func currentDirectory() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.ToSlash(dir)
}

func fileResolver(ld loader, optional bool) Resolver {
	return func(_ context.Context, node *yaml.Node) error {
		expression := node.Value
		fileName := expression
		var err error
		if strings.Contains(fileName, "$") {
			fileName, err = expandEnvWord(fileName, ld.envLookup)
			if err != nil {
				return err
			}
		}
		name, err := ld.filePath(fileName)
		if err != nil {
			return fmt.Errorf("cannot read %q (from %q): %w", name, expression, err)
		}
		data, err := fs.ReadFile(ld.fileSystem, name)
		if err != nil {
			if optional && errors.Is(err, fs.ErrNotExist) {
				return errOptionalUnset
			}
			return fmt.Errorf("cannot read %q (from %q): %w", name, expression, err)
		}
		node.Tag = ""
		node.Value = strings.TrimSpace(string(data))
		return nil
	}
}

func (ld loader) filePath(name string) (string, error) {
	var resolved string
	if ld.systemFS {
		if filepath.IsAbs(name) {
			resolved = filepath.ToSlash(filepath.Clean(name))
		} else {
			resolved = filepath.ToSlash(filepath.Join(ld.baseDir, name))
		}
		resolved = strings.TrimPrefix(resolved, "/")
	} else {
		if filepath.IsAbs(name) {
			return "", fmt.Errorf("absolute file path %q is not valid for a configured filesystem", name)
		}
		resolved = path.Join(filepath.ToSlash(ld.baseDir), filepath.ToSlash(name))
	}

	if !fs.ValidPath(resolved) {
		return "", fmt.Errorf("invalid file path %q", name)
	}
	return resolved, nil
}

func mergeNodes(base, override *yaml.Node) *yaml.Node {
	b := unwrap(base)
	o := unwrap(override)
	if b.Kind != yaml.MappingNode || o.Kind != yaml.MappingNode {
		return override
	}

	overIdx := map[string]int{}
	for i := 0; i < len(o.Content); i += 2 {
		overIdx[o.Content[i].Value] = i
	}

	for i := 0; i < len(b.Content); i += 2 {
		key := b.Content[i].Value
		j, ok := overIdx[key]
		if !ok {
			continue
		}
		if b.Content[i+1].Kind == yaml.MappingNode && o.Content[j+1].Kind == yaml.MappingNode {
			b.Content[i+1] = mergeNodes(b.Content[i+1], o.Content[j+1])
		} else {
			b.Content[i+1] = o.Content[j+1]
		}
		delete(overIdx, key)
	}

	for _, j := range overIdx {
		b.Content = append(b.Content, o.Content[j], o.Content[j+1])
	}
	return base
}

func unwrap(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// resolveOptionalNode resolves optional tags in one layer before that layer
// is merged. An unset optional mapping entry is removed, allowing an earlier
// layer to provide its value.
func resolveOptionalNode(path string, node *yaml.Node, ld loader) (bool, error) {
	if node.Kind == yaml.DocumentNode {
		return resolveOptionalNode(path, node.Content[0], ld)
	}

	if node.Kind == yaml.MappingNode {
		for i := len(node.Content) - 2; i >= 0; i -= 2 {
			key := node.Content[i].Value
			keep, err := resolveOptionalNode(fieldPath(path, key), node.Content[i+1], ld)
			if err != nil {
				return true, err
			}
			if !keep {
				node.Content = append(node.Content[:i], node.Content[i+2:]...)
			}
		}
		return true, nil
	}

	if node.Kind == yaml.SequenceNode {
		for i := len(node.Content) - 1; i >= 0; i-- {
			childPath := indexPath(path, i)
			keep, err := resolveOptionalNode(childPath, node.Content[i], ld)
			if err != nil {
				return true, err
			}
			if !keep {
				node.Content = append(node.Content[:i], node.Content[i+1:]...)
			}
		}
		return true, nil
	}

	if node.Kind != yaml.ScalarNode || !strings.HasSuffix(node.Tag, "?") {
		return true, nil
	}

	fn, ok := ld.resolvers[node.Tag]
	if !ok {
		return true, nil
	}
	if err := fn(ld.ctx, node); err != nil {
		if errors.Is(err, errOptionalUnset) {
			return false, nil
		}
		return true, resolveError(path, err)
	}
	return true, nil
}

func resolveNode(path string, node *yaml.Node, ld loader) error {
	if node.Kind == yaml.DocumentNode {
		return resolveNode(path, node.Content[0], ld)
	}

	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content)-1; i += 2 {
			key := node.Content[i].Value
			val := node.Content[i+1]
			childPath := fieldPath(path, key)
			if err := resolveNode(childPath, val, ld); err != nil {
				return err
			}
		}
		return nil
	}

	if node.Kind == yaml.SequenceNode {
		for i, child := range node.Content {
			childPath := indexPath(path, i)
			if err := resolveNode(childPath, child, ld); err != nil {
				return err
			}
		}
		return nil
	}

	if node.Kind != yaml.ScalarNode {
		return nil
	}

	return resolveScalar(path, node, ld)
}

func resolveScalar(path string, node *yaml.Node, ld loader) error {
	if fn, ok := ld.resolvers[node.Tag]; ok {
		if err := fn(ld.ctx, node); err != nil {
			return resolveError(path, err)
		}
		return nil
	}
	return nil
}

func resolveError(path string, err error) ResolveError {
	return ResolveError{Path: path, Err: err}
}

func fieldPath(path, field string) string {
	if isPathIdentifier(field) {
		return path + "." + field
	}
	return path + "[\"" + strings.ReplaceAll(strings.ReplaceAll(field, `\`, `\\`), `"`, `\"`) + "\"]"
}

func indexPath(path string, index int) string {
	return fmt.Sprintf("%s[%d]", path, index)
}

func isPathIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '_' || (i > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}
