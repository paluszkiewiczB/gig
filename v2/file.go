package gig

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type FileOption func(*fileConfig)

type fileConfig struct {
	baseDir   string
	fsys      fs.FS
	root      *os.Root
	envLookup EnvLookup
	hasFS     bool
	hasRoot   bool
}

func WithBaseDir(dir string) FileOption {
	return func(cfg *fileConfig) {
		cfg.baseDir = dir
	}
}

func WithFS(fsys fs.FS) FileOption {
	return func(cfg *fileConfig) {
		cfg.fsys = fsys
		cfg.hasFS = true
	}
}

func WithRoot(root *os.Root) FileOption {
	return func(cfg *fileConfig) {
		cfg.root = root
		cfg.hasRoot = true
	}
}

func NewFileHandler(opts ...FileOption) Mutator {
	cfg := &fileConfig{
		envLookup: os.LookupEnv,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.fsys != nil && cfg.root != nil {
		// root takes precedence if both set
		cfg.fsys = nil
	}
	if cfg.root != nil && cfg.fsys == nil {
		cfg.fsys = cfg.root.FS()
	}

	return &fileHandler{cfg: cfg}
}

func DefaultFileHandler() Mutator { return NewFileHandler() }

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

	if h.cfg.root != nil {
		content, err = readFromRoot(h.cfg.root, rawPath)
	} else if h.cfg.fsys != nil {
		content, err = readFromFS(h.cfg.fsys, rawPath)
	} else {
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
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
	}
	return string(data), nil
}

func readFromFS(fsys fs.FS, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		return rawPath, fmt.Errorf("absolute file path %q not allowed with custom fs", rawPath)
	}
	cleaned := path.Clean(rawPath)
	if strings.HasPrefix(cleaned, "..") {
		return rawPath, fmt.Errorf("invalid file path %q", rawPath)
	}
	f, err := fsys.Open(cleaned)
	if err != nil {
		return rawPath, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return rawPath, err
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
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("cannot read %q from %q", rawPath, rawPath)
	}
	return string(data), nil
}
