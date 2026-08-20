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

// FileOption configures a call to NewFileHandler.
type FileOption func(*fileConfig) error

type fileConfig struct {
	baseDir   string
	fsys      fs.FS
	root      *os.Root
	envLookup EnvLookup
}

// WithBaseDir sets the directory used to resolve relative !file paths.
func WithBaseDir(dir string) FileOption {
	return func(cfg *fileConfig) error {
		cfg.baseDir = dir
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
func WithFS(fsys fs.FS) FileOption {
	return func(cfg *fileConfig) error {
		if fsys == nil {
			return errors.New("gig: filesystem must not be nil")
		}
		cfg.fsys = fsys
		return nil
	}
}

// WithRoot sets an os.Root as the filesystem used to resolve !file paths.
// Create the root with os.OpenRoot to restrict access to a directory.
func WithRoot(root *os.Root) FileOption {
	return func(cfg *fileConfig) error {
		if root == nil {
			return errors.New("gig: root must not be nil")
		}
		cfg.root = root
		return nil
	}
}

// NewFileHandler creates a Mutator that resolves !file and !file? tags using
// the given filesystem options. When called without options, it reads from the
// system filesystem using the current working directory as the base.
func NewFileHandler(opts ...FileOption) (Mutator, error) {
	cfg := &fileConfig{
		envLookup: os.LookupEnv,
	}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	if cfg.fsys != nil && cfg.root != nil {
		// root takes precedence if both set
		cfg.fsys = nil
	}
	if cfg.root != nil && cfg.fsys == nil {
		cfg.fsys = cfg.root.FS()
	}

	return &fileHandler{cfg: cfg}, nil
}

// DefaultFileHandler returns a Mutator that resolves !file and !file? using
// the system filesystem without any custom configuration.
func DefaultFileHandler() Mutator {
	return &fileHandler{cfg: &fileConfig{envLookup: os.LookupEnv}}
}

type fileHandler struct {
	cfg *fileConfig
}

func (h *fileHandler) Mutate(ctx context.Context, node *yaml.Node) error {
	rawPath := node.Value
	optional := strings.HasSuffix(node.Tag, "?")

	expanded, err := expandPath(rawPath, h.cfg.envLookup)
	if err != nil {
		if optional && strings.Contains(err.Error(), "trailing escape") {
			return err
		}
		return err
	}
	rawPath = expanded

	var content string

	switch {
	case h.cfg.root != nil:
		content, err = readFromRoot(h.cfg.root, rawPath)
	case h.cfg.fsys != nil:
		content, err = readFromFS(h.cfg.fsys, rawPath)
	default:
		content, err = readFromSystem(rawPath, h.cfg.baseDir)
	}
	if err != nil {
		if optional {
			return ErrOptionalUnset
		}
		return err
	}

	node.Tag = ""
	node.Value = strings.TrimSpace(content)
	return nil
}

func expandPath(p string, lookup EnvLookup) (string, error) {
	parser := envParser{
		input:  p,
		lookup: lookup,
		pos:    0,
	}
	result, err := parser.evaluateWord(p)
	if err != nil {
		return "", err
	}
	return result.value, nil
}

func readFromSystem(rawPath, baseDir string) (string, error) {
	//nolint:gosec // G304: intended file read from user-provided paths
	if filepath.IsAbs(rawPath) {
		data, err := os.ReadFile(rawPath)
		if err != nil {
			return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
		}
		return string(data), nil
	}
	if baseDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
		}
		baseDir = wd
	}
	fullPath := filepath.Join(baseDir, rawPath)
	//nolint:gosec // G304: intended file read from user-provided paths
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
	}
	return string(data), nil
}

func readFromFS(fsys fs.FS, rawPath string) (ret string, retErr error) {
	if filepath.IsAbs(rawPath) {
		return rawPath, fmt.Errorf("absolute file path %q not allowed with custom fs", rawPath)
	}
	cleaned := path.Clean(rawPath)
	if strings.HasPrefix(cleaned, "..") {
		return rawPath, fmt.Errorf("invalid file path %q", rawPath)
	}
	f, err := fsys.Open(cleaned)
	if err != nil {
		return rawPath, fmt.Errorf("open %q: %w", cleaned, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()
	data, err := io.ReadAll(f)
	if err != nil {
		return rawPath, fmt.Errorf("read %q: %w", cleaned, err)
	}
	return string(data), nil
}

func readFromRoot(root *os.Root, rawPath string) (string, error) {
	cleaned := path.Clean(rawPath)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid file path %q", rawPath)
	}
	file, err := root.Open(cleaned)
	if err != nil {
		return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
	}
	data, err := io.ReadAll(file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
	}
	return string(data), nil
}
