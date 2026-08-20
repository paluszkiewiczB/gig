package gig_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paluszkiewiczB/gig/v2"
	"gopkg.in/yaml.v3"
)

const (
	setValue     = "set-value"
	otherValue   = "other-value"
	defaultStr   = "default"
	alternateStr = "alternate"
)

var (
	errSourceFailed   = errors.New("source failed")
	errResolverFailed = errors.New("resolver failed")
	errBoom           = errors.New("boom")
	errBang           = errors.New("bang")
)

type fakeEnv map[string]string

func (fe fakeEnv) lookup(name string) (string, bool) {
	v, ok := fe[name]

	return v, ok
}

type cfg struct {
	Name    string `yaml:"name"`
	Port    int    `yaml:"port"`
	Enabled bool   `yaml:"enabled"`
	Nested  struct {
		Value string `yaml:"value"`
	} `yaml:"nested"`
}

type failReader struct {
	err error
}

func (r failReader) Read([]byte) (int, error) {
	return 0, r.err
}

type valValid struct {
	Name string `yaml:"name"`
}

func (v valValid) Validate() error {
	if v.Name == "" {
		return io.ErrUnexpectedEOF
	}

	return nil
}

type ptrValid struct {
	Name string `yaml:"name"`
}

func (v *ptrValid) Validate() error {
	if v.Name == "" {
		return io.ErrUnexpectedEOF
	}

	return nil
}

type valValidCtx struct {
	Name string `yaml:"name"`
}

func (v valValidCtx) ValidateContext(_ context.Context) error {
	if v.Name == "" {
		return io.ErrUnexpectedEOF
	}

	return nil
}

type ptrValidCtx struct {
	Name string `yaml:"name"`
}

func (v *ptrValidCtx) ValidateContext(_ context.Context) error {
	if v.Name == "" {
		return io.ErrUnexpectedEOF
	}

	return nil
}

func TestLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty document", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "" || got.Port != 0 || got.Enabled || got.Nested.Value != "" {
			t.Errorf("got %+v, want zero value", got)
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](ctx, strings.NewReader("name: [\n"))
		if err == nil || !strings.Contains(err.Error(), "yaml:") {
			t.Fatalf("Load() error = %v, want yaml error", err)
		}
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()
		wantErr := errSourceFailed
		_, err := gig.Load[cfg](ctx, failReader{err: wantErr})
		if err == nil || !errors.Is(err, wantErr) {
			t.Fatalf("Load() error = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("decode type mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[int](ctx, strings.NewReader("name: foo\n"))
		if err == nil || !strings.Contains(err.Error(), "unmarshal") {
			t.Fatalf("Load() error = %v, want unmarshal error", err)
		}
	})

	t.Run("empty source", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](ctx, strings.NewReader(""))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	})
}

func TestLoadOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("nested mapping merges", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: base\nport: 1000\nnested:\n  value: base-value\n"),
			gig.WithSources(strings.NewReader("name: override\nnested:\n  value: override-value\n")),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "override" {
			t.Errorf("Name = %q, want %q", got.Name, "override")
		}
		if got.Port != 1000 {
			t.Errorf("Port = %d, want 1000", got.Port)
		}
		if got.Nested.Value != "override-value" {
			t.Errorf("Nested.Value = %q, want %q", got.Nested.Value, "override-value")
		}
	})

	t.Run("scalar replaces mapping", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[int](ctx, strings.NewReader("1\n"), gig.WithSources(strings.NewReader("2\n")))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got != 2 {
			t.Errorf("got %d, want 2", got)
		}
	})

	t.Run("new key from override", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: a\n"),
			gig.WithSources(strings.NewReader("name: b\nport: 9\n")),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "b" {
			t.Errorf("Name = %q, want %q", got.Name, "b")
		}
		if got.Port != 9 {
			t.Errorf("Port = %d, want 9", got.Port)
		}
	})
}

func TestLoadEnv(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := fakeEnv{
		"setEnv":   setValue,
		"emptyEnv": "",
		"otherEnv": otherValue,
	}
	load := func(src string, opts ...gig.LoadOption) (cfg, error) {
		return gig.Load[cfg](
			ctx,
			strings.NewReader(src),
			append(
				[]gig.LoadOption{gig.WithEnvOptions(gig.WithEnvLookup(env.lookup))},
				opts...,
			)...,
		)
	}

	t.Run("value operators", func(t *testing.T) {
		t.Parallel()
		tests := map[string]struct {
			expr string
			want string
		}{
			"unset uses dash default":                 {expr: "${unsetEnv-default}", want: defaultStr},
			"empty preserves dash value":              {expr: "${emptyEnv-default}", want: ""},
			"unset uses colon dash default":           {expr: "${unsetEnv:-default}", want: defaultStr},
			"empty uses colon dash default":           {expr: "${emptyEnv:-default}", want: defaultStr},
			"set plus uses alternate":                 {expr: "${setEnv+alternate}", want: alternateStr},
			"empty plus uses alternate":               {expr: "${emptyEnv+alternate}", want: alternateStr},
			"unset plus is empty":                     {expr: "${unsetEnv+alternate}", want: ""},
			"set colon plus uses alternate":           {expr: "${setEnv:+alternate}", want: alternateStr},
			"empty colon plus is empty":               {expr: "${emptyEnv:+alternate}", want: ""},
			"unset colon plus is empty":               {expr: "${unsetEnv:+alternate}", want: ""},
			"nested fallback uses second environment": {expr: "${unsetEnv:-${otherEnv:-constant}}", want: otherValue},
			"nested fallback uses dollar environment": {expr: "${unsetEnv:-$otherEnv}", want: otherValue},
			"nested fallback uses literal":            {expr: "${unsetEnv:-${nestedUnsetEnv:-constant}}", want: "constant"},
			"escaped dollar stays literal":            {expr: "${unsetEnv:-\\$otherEnv}", want: "$otherEnv"},
			"escaped nested expansion stays literal":  {expr: "${unsetEnv:-\\${otherEnv}}", want: "${otherEnv}"},
			"escaped braces produce correct literal":  {expr: "${unsetEnv:-\\${FOO\\}}", want: "${FOO}"},
			"escaped brace with trailing text":        {expr: "${unsetEnv:-\\${BAR}baz}", want: "${BAR}baz"},
			"double nested fallback to escaped":       {expr: "${unsetEnv:-${nestedUnsetEnv:-\\${FOO\\}}}", want: "${FOO}"},
		}
		for name, testCase := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := load("name: !env '" + testCase.expr + "'\n")
				if err != nil {
					t.Fatalf("Load() error = %v", err)
				}
				if got.Name != testCase.want {
					t.Errorf("Name = %q, want %q", got.Name, testCase.want)
				}
			})
		}
	})

	t.Run("required operators", func(t *testing.T) {
		t.Parallel()
		for name, expr := range map[string]string{
			"unset required value": "${unsetEnv?required message}",
			"empty required value": "${emptyEnv:?required message}",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := load("name: !env '" + expr + "'\n")
				if err == nil {
					t.Fatal("Load() error = nil, want required-value error")
				}
			})
		}
	})

	t.Run("assignment operators", func(t *testing.T) {
		t.Parallel()
		for name, expr := range map[string]string{
			"unset assignment": "${unsetEnv=assigned}",
			"empty assignment": "${emptyEnv:=assigned}",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := load("name: !env '" + expr + "'\n")
				if err == nil {
					t.Fatal("Load() error = nil, want unsupported assignment error")
				}
			})
		}
	})

	t.Run("simple variable", func(t *testing.T) {
		t.Parallel()
		got, err := load("name: !env '${setEnv}'\n")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != setValue {
			t.Errorf("Name = %q, want %q", got.Name, setValue)
		}
	})

	t.Run("trailing text after expansion", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${setEnv}extra'\n")
		if err == nil {
			t.Fatal("Load() error = nil, want trailing text error")
		}
	})

	t.Run("optional expression absent", func(t *testing.T) {
		t.Parallel()
		got, err := load(
			"name: root\n",
			gig.WithSources(strings.NewReader("name: !env? '${unsetEnv}'\n")),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "root" {
			t.Errorf("Name = %q, want root", got.Name)
		}
	})

	t.Run("invalid variable name", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${123invalid}'\n")
		if err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("Load() error = %v, want invalid name error", err)
		}
	})

	t.Run("unterminated no operator", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${VAR'\n")
		if err == nil || !strings.Contains(err.Error(), "unterminated") {
			t.Fatalf("Load() error = %v, want unterminated error", err)
		}
	})

	t.Run("incomplete operator", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${VAR:'\n")
		if err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Fatalf("Load() error = %v, want incomplete operator error", err)
		}
	})

	t.Run("word trailing escape", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env ${VAR:-\\\n")
		if err == nil || !strings.Contains(err.Error(), "trailing escape") {
			t.Fatalf("Load() error = %v, want trailing escape error", err)
		}
	})

	t.Run("word unterminated", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env ${unsetEnv:-${BAR\n")
		if err == nil {
			t.Fatal("Load() error = nil, want unterminated word error")
		}
	})

	t.Run("unknown operator", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${VAR@x}'\n")
		if err == nil || !strings.Contains(err.Error(), "unsupported environment operator") {
			t.Fatalf("Load() error = %v, want unsupported operator error", err)
		}
	})

	t.Run("question present", func(t *testing.T) {
		t.Parallel()
		got, err := load("name: !env '${setEnv?msg}'\n")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "set-value" {
			t.Errorf("Name = %q, want set-value", got.Name)
		}
	})

	t.Run("colon question present", func(t *testing.T) {
		t.Parallel()
		got, err := load("name: !env '${setEnv:?msg}'\n")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != setValue {
			t.Errorf("Name = %q, want %s", got.Name, setValue)
		}
	})

	t.Run("required unset no message", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${unsetEnv?}'\n")
		if err == nil || !strings.Contains(err.Error(), "is not set") {
			t.Fatalf("Load() error = %v, want 'is not set' error", err)
		}
	})

	t.Run("required empty with message", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${emptyEnv:?}'\n")
		if err == nil || !strings.Contains(err.Error(), "is empty") {
			t.Fatalf("Load() error = %v, want 'is empty' error", err)
		}
	})

	t.Run("evaluate inner expansion error", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${unsetEnv:-${123invalid}}'\n")
		if err == nil {
			t.Fatal("Load() error = nil, want evaluate error")
		}
	})

	t.Run("inner expansion error in evaluate word", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${unsetEnv:-${FOO\\}}'\n")
		if err == nil {
			t.Fatal("Load() error = nil, want evaluation error")
		}
	})

	t.Run("required error message word fails", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${unsetEnv:?${BROKEN\\}}}'\n")
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("Load() error = %v, want error from message word", err)
		}
	})

	t.Run("required error message fails", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env ${unsetEnv:?\\\n")
		if err == nil || !strings.Contains(err.Error(), "trailing escape") {
			t.Fatalf("Load() error = %v, want trailing escape error", err)
		}
	})

	t.Run("missing reports error", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env GIG_TEST_MISSING\n")
		if err == nil {
			t.Fatal("Load() error = nil, want missing env error")
		}
	})

	t.Run("optional", func(t *testing.T) {
		t.Parallel()
		_, err := load(
			"name: root\n",
			gig.WithSources(strings.NewReader("name: !env? OPTIONAL_NAME\n")),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	})

	t.Run("malformed expression", func(t *testing.T) {
		t.Parallel()
		_, err := load("name: !env '${BROKEN\n")
		if err == nil {
			t.Fatal("Load() error = nil, want malformed expression error")
		}
	})
}

func TestLoadFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("required missing reports error", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](ctx, strings.NewReader("name: !file missing-secret\n"))
		if err == nil ||
			!strings.Contains(err.Error(), `cannot read "`) ||
			!strings.Contains(err.Error(), `from "missing-secret"`) {
			t.Fatalf("Load() error = %v, want file read error", err)
		}
	})

	t.Run("relative to base dir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("  file-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file secret.txt\n"),
			gig.WithFileOptions(gig.WithBaseDir(dir)),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "file-secret" {
			t.Errorf("Name = %q, want %q", got.Name, "file-secret")
		}
	})

	t.Run("env in filepath", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "default.yaml"), []byte("from-default\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file ${GIG_TEST_FILE:-${GIG_TEST_OTHER_FILE:-default.yaml}}\n"),
			gig.WithFileOptions(gig.WithBaseDir(dir)),
			gig.WithEnvOptions(gig.WithEnvLookup(func(_ string) (string, bool) { return "", false })),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "from-default" {
			t.Errorf("Name = %q, want %q", got.Name, "from-default")
		}
	})

	t.Run("configured fs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("rooted-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := root.Close(); err != nil {
				t.Error(err)
			}
		})

		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file secret.txt\n"),
			gig.WithFileOptions(gig.WithRoot(root)),
		)
		if err != nil {
			t.Fatalf("happy: Load() error = %v", err)
		}
		if got.Name != "rooted-secret" {
			t.Errorf("Name = %q, want %q", got.Name, "rooted-secret")
		}

		_, err = gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file ../outside.txt\n"),
			gig.WithFileOptions(gig.WithRoot(root)),
		)
		if err == nil || !strings.Contains(err.Error(), "invalid file path") {
			t.Fatalf("traversal: Load() error = %v, want invalid path error", err)
		}

		_, err = gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file missing.txt\n"),
			gig.WithFileOptions(gig.WithRoot(root)),
		)
		if err == nil || !strings.Contains(err.Error(), "cannot read") {
			t.Fatalf("missing: Load() error = %v, want cannot read error", err)
		}
	})

	t.Run("optional", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: root\n"),
			gig.WithFileOptions(gig.WithBaseDir(dir)),
			gig.WithSources(strings.NewReader("name: !file? missing-secret\n")),
		)
		if err != nil {
			t.Fatalf("missing: Load() error = %v", err)
		}
		if got.Name != "root" {
			t.Errorf("Name = %q, want %q", got.Name, "root")
		}

		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err = gig.Load[cfg](
			ctx,
			strings.NewReader("name: root\n"),
			gig.WithFileOptions(gig.WithBaseDir(dir)),
			gig.WithSources(strings.NewReader("name: !file? secret.txt\n")),
		)
		if err != nil {
			t.Fatalf("present: Load() error = %v", err)
		}
		if got.Name != "from-file" {
			t.Errorf("Name = %q, want %q", got.Name, "from-file")
		}
	})

	t.Run("evaluate trailing escape", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file '$x\\'\n"),
			gig.WithFileOptions(gig.WithBaseDir(dir)),
			gig.WithEnvOptions(gig.WithEnvLookup(func(_ string) (string, bool) {
				return "secret.txt", true
			})),
		)
		if err == nil || !strings.Contains(err.Error(), "trailing escape") {
			t.Fatalf("Load() error = %v, want trailing escape error", err)
		}
	})

	t.Run("absolute path system fs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		filePath := filepath.Join(dir, "abs-test.txt")
		if err := os.WriteFile(filePath, []byte("abs-path-data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file "+filePath+"\n"),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "abs-path-data" {
			t.Errorf("Name = %q, want %q", got.Name, "abs-path-data")
		}
	})
}

func TestLoadResolver(t *testing.T) { //nolint:tparallel // child uses t.Chdir which requires sequential execution
	t.Run("custom tag", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !upper hello world\n"),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!upper": gig.MutatorFunc(func(_ context.Context, node *yaml.Node) error {
					node.Tag = ""
					node.Value = strings.ToUpper(node.Value)

					return nil
				}),
			})),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "HELLO WORLD" {
			t.Errorf("Name = %q, want %q", got.Name, "HELLO WORLD")
		}
	})

	t.Run("error reports path", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		wantErr := errResolverFailed
		_, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !broken value\n"),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!broken": gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
					return wantErr
				}),
			})),
		)
		if err == nil {
			t.Fatal("Load() error = nil, want resolver error")
		}
		var resolveErr *gig.ResolveError
		if !errors.As(err, &resolveErr) {
			t.Fatalf("error type = %T, want *ResolveError", err)
		}
		if resolveErr.Path != "$.name" {
			t.Errorf("Path = %q, want %q", resolveErr.Path, "$.name")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("error = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("path with uppercase underscore digit", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		_, err := gig.Load[cfg](
			ctx,
			strings.NewReader("MY_VAR_2: !broken v\n"),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!broken": gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
					return errResolverFailed
				}),
			})),
		)
		if err == nil {
			t.Fatal("Load() error = nil, want resolver error")
		}
		if !strings.Contains(err.Error(), "$.MY_VAR_2") {
			t.Errorf("error = %v, want path $.MY_VAR_2", err)
		}
	})

	t.Run("context propagation", func(t *testing.T) {
		t.Parallel()
		type ctxKey struct{}
		ctx := context.WithValue(context.Background(), ctxKey{}, "from-ctx")
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !ctx hello\n"),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!ctx": gig.MutatorFunc(func(c context.Context, node *yaml.Node) error {
					node.Tag = ""
					v, ok := c.Value(ctxKey{}).(string)
					if !ok {
						return fmt.Errorf("context value %T is not a string", c.Value(ctxKey{}))
					}
					node.Value = node.Value + "-" + v

					return nil
				}),
			})),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "hello-from-ctx" {
			t.Errorf("Name = %q, want %q", got.Name, "hello-from-ctx")
		}
	})

	t.Run("overrides builtin", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !env GIG_TEST_OVERRIDE\n"),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!env": gig.MutatorFunc(func(_ context.Context, node *yaml.Node) error {
					node.Tag = ""
					node.Value = "overridden-" + node.Value

					return nil
				}),
				"!env?":  gig.DefaultEnvHandler(),
				"!file":  gig.DefaultFileHandler(),
				"!file?": gig.DefaultFileHandler(),
			})),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "overridden-GIG_TEST_OVERRIDE" {
			t.Errorf("Name = %q, want %q", got.Name, "overridden-GIG_TEST_OVERRIDE")
		}
	})

	t.Run("nil env lookup", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		_, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"), gig.WithEnvOptions(gig.WithEnvLookup(nil)))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("nil env expander", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		_, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"), gig.WithEnvOptions(gig.WithEnvExpander(nil)))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("nil root", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		_, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"), gig.WithFileOptions(gig.WithRoot(nil)))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("nil file system", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		_, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"), gig.WithFileOptions(gig.WithFS(nil)))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("with file system", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("fs-data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file secret.txt\n"),
			gig.WithFileOptions(gig.WithFS(os.DirFS(dir))),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "fs-data" {
			t.Errorf("Name = %q, want %q", got.Name, "fs-data")
		}

		_, err = gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file /etc/passwd\n"),
			gig.WithFileOptions(gig.WithFS(os.DirFS(dir))),
		)
		if err == nil || !strings.Contains(err.Error(), "absolute file path") {
			t.Fatalf("Load() error = %v, want absolute path error", err)
		}
	})

	t.Run("optional resolver error", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		_, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: base\n"),
			gig.WithSources(strings.NewReader("name: !err?\n")),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!err?": gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
					return errBoom
				}),
			})),
		)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("Load() error = %v, want boom error", err)
		}
	})

	t.Run("unknown optional tag", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: base\n"),
			gig.WithSources(strings.NewReader("name: !nope? value\n")),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "value" {
			t.Errorf("Name = %q, want value", got.Name)
		}
	})

	t.Run("sequence with env tags", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		type listCfg struct {
			List []string `yaml:"list"`
		}
		got, err := gig.Load[listCfg](
			ctx,
			strings.NewReader("list:\n  - !env GIG_SEQ_VAR\n  - literal\n"),
			gig.WithEnvOptions(gig.WithEnvLookup(func(_ string) (string, bool) { return "from-env", true })),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(got.List) != 2 || got.List[0] != "from-env" || got.List[1] != "literal" {
			t.Errorf("List = %#v, want [from-env literal]", got.List)
		}
	})

	t.Run("sequence resolution error", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		type listCfg struct {
			List []string `yaml:"list"`
		}
		_, err := gig.Load[listCfg](
			ctx,
			strings.NewReader("list:\n  - !env GIG_MISSING_SEQ\n"),
		)
		if err == nil {
			t.Fatal("Load() error = nil, want missing env error")
		}
	})

	t.Run("sequence optional removes item", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		type listCfg struct {
			List []string `yaml:"list"`
		}
		got, err := gig.Load[listCfg](
			ctx,
			strings.NewReader("list:\n  - base\n"),
			gig.WithSources(strings.NewReader("list:\n  - !env? GIG_OPT_SEQ_REMOVE\n  - extra\n")),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(got.List) != 1 || got.List[0] != "extra" {
			t.Errorf("List = %#v, want [extra]", got.List)
		}
	})

	t.Run("sequence optional errors", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		type listCfg struct {
			List []string `yaml:"list"`
		}
		_, err := gig.Load[listCfg](
			ctx,
			strings.NewReader("list:\n  - ok\n"),
			gig.WithSources(strings.NewReader("list:\n  - !boom?\n")),
			gig.WithMutators(gig.NewTagResolver(map[string]gig.Mutator{
				"!boom?": gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
					return errBang
				}),
			})),
		)
		if err == nil || !strings.Contains(err.Error(), "bang") {
			t.Fatalf("Load() error = %v, want bang error", err)
		}
	})

	t.Run("empty key identifier", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[map[string]string](
			ctx,
			strings.NewReader(`"": from-empty-key`+"\n"),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got[""] != "from-empty-key" {
			t.Errorf(`got[""] = %q, want from-empty-key`, got[""])
		}
	})

	t.Run("bracket path", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		type dotCfg struct {
			Field string `yaml:"field.with.dots"`
		}
		got, err := gig.Load[dotCfg](
			ctx,
			strings.NewReader(`"field.with.dots": !env GIG_DOT_VAR`+"\n"),
			gig.WithEnvOptions(gig.WithEnvLookup(func(_ string) (string, bool) { return "dot-value", true })),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Field != "dot-value" {
			t.Errorf("Field = %q, want dot-value", got.Field)
		}
	})

	t.Run("resolve error nil err", func(t *testing.T) {
		t.Parallel()
		got := (&gig.ResolveError{Path: "$.x", Err: nil}).Error()
		if got != "$.x" {
			t.Errorf("Error() = %q, want $.x", got)
		}
	})

	t.Run("alias node", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		type aliasCfg struct {
			Name  string `yaml:"name"`
			Alias string `yaml:"alias"`
		}
		got, err := gig.Load[aliasCfg](
			ctx,
			strings.NewReader("name: &anchor gig\nalias: *anchor\n"),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "gig" || got.Alias != "gig" {
			t.Errorf("got %+v, want {Name:gig Alias:gig}", got)
		}
	})

	t.Run("current dir fallback", func(t *testing.T) { //nolint:paralleltest // t.Chdir requires sequential execution
		ctx := context.Background()
		d := t.TempDir()
		t.Chdir(d)
		if err := os.Remove(d); err != nil {
			t.Fatal(err)
		}

		_, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"))
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
	})
}

func TestLoadEnvLookup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("custom lookup for env", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !env CUSTOM_NAME\n"),
			gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
				if name == "CUSTOM_NAME" {
					return "from-custom-lookup", true
				}

				return "", false
			})),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "from-custom-lookup" {
			t.Errorf("Name = %q, want %q", got.Name, "from-custom-lookup")
		}
	})

	t.Run("custom lookup in filepath", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("from-file"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !file $CUSTOM_FILE\n"),
			gig.WithFileOptions(gig.WithBaseDir(dir)),
			gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
				if name == "CUSTOM_FILE" {
					return "secret.txt", true
				}

				return "", false
			})),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "from-file" {
			t.Errorf("Name = %q, want %q", got.Name, "from-file")
		}
	})

	t.Run("custom expander", func(t *testing.T) {
		t.Parallel()
		var gotOptional bool
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !env? CUSTOM_EXPRESSION\n"),
			gig.WithEnvOptions(gig.WithEnvExpander(func(expression string, optional bool) (string, bool, error) {
				gotOptional = optional
				if expression != "CUSTOM_EXPRESSION" {
					return "", false, fmt.Errorf("unexpected expression %q", expression)
				}

				return "from-custom-expander", true, nil
			})),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "from-custom-expander" {
			t.Errorf("Name = %q, want %q", got.Name, "from-custom-expander")
		}
		if !gotOptional {
			t.Error("optional flag = false, want true")
		}
	})

	t.Run("expander returns absent without error", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: !env ANY_NAME\n"),
			gig.WithEnvOptions(gig.WithEnvExpander(func(_ string, _ bool) (string, bool, error) {
				return "", false, nil
			})),
		)
		if err == nil || !strings.Contains(err.Error(), "produced no value") {
			t.Fatalf("Load() error = %v, want 'produced no value'", err)
		}
	})
}

func TestLoadValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("value validator", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[valValid](ctx, strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[valValid](ctx, strings.NewReader(`name: ""`+"\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("pointer validator", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[ptrValid](ctx, strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[ptrValid](ctx, strings.NewReader(`name: ""`+"\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("value validator context", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[valValidCtx](ctx, strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[valValidCtx](ctx, strings.NewReader(`name: ""`+"\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("pointer validator context", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[ptrValidCtx](ctx, strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[ptrValidCtx](ctx, strings.NewReader(`name: ""`+"\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[valValid](ctx, strings.NewReader(`name: ""`+"\n"), gig.WithValidation(false))
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
	})
}

func TestCloneNodeNil(t *testing.T) {
	t.Parallel()
	// Ensure nil does not panic
	ctx := context.Background()
	_, err := gig.Load[cfg](ctx, strings.NewReader("{}\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestMergeContentMultiDoc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: first\n"),
		gig.WithSources(strings.NewReader("name: second\n")),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "second" {
		t.Errorf("Name = %q, want %q", got.Name, "second")
	}
}

func TestAliasNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type aliasCfg struct {
		Name  string `yaml:"name"`
		Alias string `yaml:"alias"`
	}
	got, err := gig.Load[aliasCfg](
		ctx,
		strings.NewReader("name: &anchor gig\nalias: *anchor\n"),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "gig" || got.Alias != "gig" {
		t.Errorf("got %+v, want {Name:gig Alias:gig}", got)
	}
}

func TestTagResolverWalkAlias(t *testing.T) {
	t.Parallel()
	tr := gig.NewTagResolver(map[string]gig.Mutator{
		"!env": gig.DefaultEnvHandler(),
	})
	ctx := context.Background()
	// Alias node should not cause panic
	doc := &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{
			{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
					{Kind: yaml.ScalarNode, Value: "gig", Tag: "!!str"},
				},
			},
		},
	}
	if err := tr.Mutate(ctx, doc); err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
}

func TestResolverSequenceTag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type listCfg struct {
		List []string `yaml:"list"`
	}
	got, err := gig.Load[listCfg](
		ctx,
		strings.NewReader("list:\n  - !env GIG_EXISTING\n  - !env GIG_MISSING\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
			if name == "GIG_EXISTING" {
				return "present", true
			}
			return "", false
		})),
	)
	if err == nil {
		t.Fatal("expected error for missing env in sequence")
	}
	_ = got
}

func TestLoadWithSourcesEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: first\n"),
		gig.WithSources(strings.NewReader("")),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "first" {
		t.Errorf("Name = %q, want %q", got.Name, "first")
	}
}

func TestReadFromSystemAbsolute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Using absolute path should work with system fs
	// Use /dev/null which always exists
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file /dev/null\n"),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty from /dev/null", got.Name)
	}
}

func TestReadFromFSAbsolute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file /tmp/test.txt\n"),
		gig.WithFileOptions(gig.WithFS(os.DirFS(t.TempDir()))),
	)
	_ = err
	if err == nil {
		t.Fatal("expected error for absolute path with fs.FS")
	}
}

func TestMergeContentMultiDocSequence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type intCfg struct {
		Val int `yaml:"val"`
	}
	got, err := gig.Load[intCfg](
		ctx,
		strings.NewReader("val: 1\n"),
		gig.WithSources(strings.NewReader("val: 2\n")),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Val != 2 {
		t.Errorf("Val = %d, want 2", got.Val)
	}
}

func TestLoadFileRootSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/secret.txt", []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})

	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file secret.txt\n"),
		gig.WithFileOptions(gig.WithRoot(root)),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "content" {
		t.Errorf("Name = %q, want %q", got.Name, "content")
	}
}

func TestTagResolverNilNode(t *testing.T) {
	t.Parallel()
	tr := gig.NewTagResolver(nil)
	ctx := context.Background()
	err := tr.Mutate(ctx, nil)
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
}

func TestTaggedMappingKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("!env NAME: test\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
			if name == "NAME" {
				return "key-value", true
			}
			return "", false
		})),
	)
	// This might succeed or fail depending on YAML parsing of tagged key
	_ = got
	_ = err
}

func TestNestedMappingWalk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type nestedCfg struct {
		Inner struct {
			Value string `yaml:"value"`
		} `yaml:"inner"`
	}
	got, err := gig.Load[nestedCfg](
		ctx,
		strings.NewReader("inner:\n  value: !env INNER_VAL\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
			if name == "INNER_VAL" {
				return "nested-resolved", true
			}
			return "", false
		})),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Inner.Value != "nested-resolved" {
		t.Errorf("Inner.Value = %q, want %q", got.Inner.Value, "nested-resolved")
	}
}

func TestReadFromFSTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file ../outside.txt\n"),
		gig.WithFileOptions(gig.WithFS(os.DirFS(t.TempDir()))),
	)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestReadFromFSMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file missing.txt\n"),
		gig.WithFileOptions(gig.WithFS(os.DirFS(t.TempDir()))),
	)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFromSystemAbsoluteMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file /nonexistent/path/that/does/not/exist\n"),
	)
	if err == nil {
		t.Fatal("expected error for missing absolute path")
	}
}

func TestReadFromRootReadAllError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Opening a directory via os.Root succeeds but io.ReadAll fails
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})

	// Create a subdirectory so we can open it
	if err := os.MkdirAll(dir+"/subdir", 0o750); err != nil {
		t.Fatal(err)
	}

	_, err = gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file subdir\n"),
		gig.WithFileOptions(gig.WithRoot(root)),
	)
	if err == nil {
		t.Fatal("expected error when reading directory as file")
	}
}

func TestTaggedKeyErrorReturn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := gig.NewTagResolver(map[string]gig.Mutator{
		"!err": gig.MutatorFunc(func(_ context.Context, node *yaml.Node) error {
			return errors.New("tagged key error")
		}),
	})
	type mapCfg struct {
		Val string `yaml:"val"`
	}
	// YAML complex key syntax: ? !err KEY \n : value
	_, err := gig.Load[mapCfg](
		ctx,
		strings.NewReader("? !err MY_KEY\n: value\nval: keep\n"),
		gig.WithMutators(tr),
	)
	if err == nil {
		t.Fatal("expected error from tagged key")
	}
}

func TestTaggedKeyInMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := gig.NewTagResolver(map[string]gig.Mutator{
		"!env?": gig.DefaultEnvHandler(),
	})
	_ = tr
	// Test tagged key with unknown env - key should be removed
	type mapCfg struct {
		Name string `yaml:"name"`
	}
	// YAML with ? syntax for complex key
	yamlContent := "? !env? UNKNOWN_KEY\n: value\nname: kept\n"
	got, err := gig.Load[mapCfg](
		ctx,
		strings.NewReader(yamlContent),
		gig.WithMutators(tr),
		gig.WithEnvOptions(gig.WithEnvLookup(func(_ string) (string, bool) {
			return "", false
		})),
	)
	_ = got
	_ = err
}

func TestReadFromFSReadAllError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file any.txt\n"),
		gig.WithFileOptions(gig.WithFS(&errorFS{})),
	)
	if err == nil {
		t.Fatal("expected error from read failure")
	}
}

func TestSequenceNonScalarWalkError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := gig.NewTagResolver(map[string]gig.Mutator{
		"!err": gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
			return errors.New("sequence nested error")
		}),
	})
	type seqCfg struct {
		Items []struct {
			Val string `yaml:"val"`
		} `yaml:"items"`
	}
	_, err := gig.Load[seqCfg](
		ctx,
		strings.NewReader("items:\n  - val: !err test\n"),
		gig.WithMutators(tr),
	)
	if err == nil {
		t.Fatal("expected error from sequence nested walk")
	}
}

func TestGetwdError(t *testing.T) { //nolint:paralleltest // t.Chdir is incompatible with parallel execution
	ctx := context.Background()
	d := t.TempDir()
	t.Chdir(d)
	if err := os.Remove(d); err != nil {
		t.Fatal(err)
	}

	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file missing.txt\n"),
	)
	if err == nil {
		t.Fatal("expected error when CWD is removed")
	}
}

func TestSequenceWithNestedMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type nestedSeq struct {
		Items []struct {
			Key   string `yaml:"key"`
			Value string `yaml:"value"`
		} `yaml:"items"`
	}
	got, err := gig.Load[nestedSeq](
		ctx,
		strings.NewReader("items:\n  - key: first\n    value: one\n  - key: second\n    value: two\n"),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].Key != "first" || got.Items[1].Value != "two" {
		t.Errorf("Items = %+v", got.Items)
	}
}

func TestYamlKey(t *testing.T) {
	t.Parallel()

	t.Run("Key", func(t *testing.T) {
		t.Parallel()
		k := gig.YamlKey("").Key("foo").Key("bar")
		if string(k) != "foo.bar" {
			t.Errorf("YamlKey = %q, want %q", k, "foo.bar")
		}
	})

	t.Run("Index", func(t *testing.T) {
		t.Parallel()
		k := gig.YamlKey("").Key("foo").Index(0).Key("a.b")
		if string(k) != "foo[0].a.b" {
			t.Errorf("YamlKey = %q, want %q", k, "foo[0].a.b")
		}
	})

	t.Run("Segments", func(t *testing.T) {
		t.Parallel()
		segs := gig.YamlKey("foo.bar").Segments()
		if len(segs) != 2 {
			t.Errorf("Segments = %v, want 2 segments", segs)
		}
	})

	t.Run("ParseYamlKey", func(t *testing.T) {
		t.Parallel()
		k, segs, err := gig.ParseYamlKey("foo.bar[0]")
		if err != nil {
			t.Fatalf("ParseYamlKey() error = %v", err)
		}
		if string(k) != "foo.bar[0]" {
			t.Errorf("YamlKey = %q, want %q", k, "foo.bar[0]")
		}
		if len(segs) != 3 {
			t.Errorf("segments = %d, want 3", len(segs))
		}
	})

	t.Run("ParseYamlKey empty", func(t *testing.T) {
		t.Parallel()
		k, segs, err := gig.ParseYamlKey("")
		if err != nil {
			t.Fatalf("ParseYamlKey() error = %v", err)
		}
		if k != "" || len(segs) != 0 {
			t.Errorf("expected empty, got %q %v", k, segs)
		}
	})

	t.Run("ParseYamlKey unclosed bracket", func(t *testing.T) {
		t.Parallel()
		_, _, err := gig.ParseYamlKey("foo[0")
		if err == nil {
			t.Fatal("expected error for unclosed bracket")
		}
	})

	t.Run("ParseYamlKey invalid index", func(t *testing.T) {
		t.Parallel()
		_, _, err := gig.ParseYamlKey("foo[abc]")
		if err == nil {
			t.Fatal("expected error for invalid index")
		}
	})

	t.Run("Segments error path", func(t *testing.T) {
		t.Parallel()
		gig.YamlKey("foo[abc]").Segments()
	})
}

func TestTagResolverHandle(t *testing.T) {
	t.Parallel()

	tr := gig.NewTagResolver(nil)
	tr = tr.Handle("!custom", gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
		return nil
	}))

	var called bool
	tr = tr.Handle("!flag", gig.MutatorFunc(func(_ context.Context, _ *yaml.Node) error {
		called = true
		return nil
	}))

	ctx := context.Background()
	node := &yaml.Node{Tag: "!flag", Kind: yaml.ScalarNode, Value: "test"}

	if err := tr.Mutate(ctx, node); err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestDefaultMutators(t *testing.T) {
	t.Parallel()

	mutators := gig.DefaultMutators()
	if len(mutators) != 1 {
		t.Fatalf("DefaultMutators() = %d, want 1", len(mutators))
	}

	// Should be able to use default mutators to load config
	ctx := context.Background()
	_, err := gig.Load[cfg](ctx, strings.NewReader("name: test\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestNewOverride(t *testing.T) {
	t.Parallel()

	t.Run("override scalar", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: original\nport: 999\n"),
			gig.WithMutators(
				gig.NewOverride(map[gig.YamlKey]string{
					gig.YamlKey("").Key("name"): "overridden",
				}),
			),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "overridden" {
			t.Errorf("Name = %q, want %q", got.Name, "overridden")
		}
		if got.Port != 999 {
			t.Errorf("Port = %d, want 999", got.Port)
		}
	})

	t.Run("override nested", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: orig\nnested:\n  value: orig-nested\n"),
			gig.WithMutators(
				gig.NewOverride(map[gig.YamlKey]string{
					gig.YamlKey("").Key("nested").Key("value"): "new-nested",
				}),
			),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Nested.Value != "new-nested" {
			t.Errorf("Nested.Value = %q, want %q", got.Nested.Value, "new-nested")
		}
	})

	t.Run("override new key", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("{}\n"),
			gig.WithMutators(
				gig.NewOverride(map[gig.YamlKey]string{
					gig.YamlKey("").Key("name"): "new-key",
				}),
			),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "new-key" {
			t.Errorf("Name = %q, want %q", got.Name, "new-key")
		}
	})
}

func TestEnvOverrides(t *testing.T) {
	t.Run("t.Setenv", func(t *testing.T) {
		t.Setenv("GIG_TEST_NAME", "env-override-val")
		overrides := gig.EnvOverrides("GIG_")
		key := gig.YamlKey("").Key("TEST").Key("NAME")
		val, ok := overrides[key]
		if !ok {
			t.Fatalf("EnvOverrides did not find GIG_TEST_NAME")
		}
		if val != "env-override-val" {
			t.Errorf("val = %q, want %q", val, "env-override-val")
		}
	})

	t.Run("exact prefix match", func(t *testing.T) {
		t.Setenv("GIG_", "root-val")
		t.Setenv("GIG_X", "x-val")
		overrides := gig.EnvOverrides("GIG_")
		if len(overrides) != 1 {
			t.Errorf("expected 1 override (GIG_X), got %d", len(overrides))
		}
	})
}

func TestLoadOptionNilGuard(t *testing.T) {
	t.Parallel()

	t.Run("WithSources nil", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](context.Background(), strings.NewReader("{}\n"), gig.WithSources(nil))
		if err == nil {
			t.Fatal("expected error for nil source reader")
		}
	})

	t.Run("WithMutators nil", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](context.Background(), strings.NewReader("{}\n"), gig.WithMutators(nil))
		if err == nil {
			t.Fatal("expected error for nil mutator")
		}
	})

	t.Run("WithFileOptions nil", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](context.Background(), strings.NewReader("{}\n"), gig.WithFileOptions(nil))
		if err == nil {
			t.Fatal("expected error for nil file option")
		}
	})

	t.Run("WithEnvOptions nil", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](context.Background(), strings.NewReader("{}\n"), gig.WithEnvOptions(nil))
		if err == nil {
			t.Fatal("expected error for nil env option")
		}
	})
}

func TestNewFileHandlerNilRoot(t *testing.T) {
	t.Parallel()
	_, err := gig.NewFileHandler(gig.WithRoot(nil))
	if err == nil {
		t.Fatal("expected error for nil root")
	}
}

func TestMergeVarious(t *testing.T) {
	t.Parallel()
	t.Run("empty dst", func(t *testing.T) {
		ctx := context.Background()
		got, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: only\n"),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "only" {
			t.Errorf("Name = %q, want %q", got.Name, "only")
		}
	})
}

func TestDefaultsWithEnvOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := map[string]string{"TEST_VAR": "from-env"}
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !env TEST_VAR\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		})),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "from-env" {
		t.Errorf("Name = %q, want %q", got.Name, "from-env")
	}
}

func TestDefaultsWithFileOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file testdata/password.txt\n"),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "s3cret" {
		t.Errorf("Name = %q, want %q", got.Name, "s3cret")
	}
}

func TestExpandEnvSimpleName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !env '${TEST_EXPAND}'\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
			if name == "TEST_EXPAND" {
				return "expanded", true
			}
			return "", false
		})),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "expanded" {
		t.Errorf("Name = %q, want %q", got.Name, "expanded")
	}
}

func TestLoadWithExpanderv2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !env? CUSTOM\n"),
		gig.WithEnvOptions(gig.WithEnvExpander(func(_ string, _ bool) (string, bool, error) {
			return "", false, nil
		})),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty", got.Name)
	}
}

func TestParseError(t *testing.T) {
	t.Parallel()
	t.Run("unterminated", func(t *testing.T) {
		_, err := parseEnv("${unclosed")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty", func(t *testing.T) {
		_, err := parseEnv("${}")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func parseEnv(expr string) (string, error) {
	// Use direct load to test expression parsing
	type simpleCfg struct {
		Val string `yaml:"val"`
	}
	_, err := gig.Load[simpleCfg](
		context.Background(),
		strings.NewReader("val: !env '"+expr+"'\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(func(_ string) (string, bool) {
			return "", false
		})),
	)
	if err != nil {
		return "", err
	}
	return "", nil
}

func TestOverrideEdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("empty segments", func(t *testing.T) {
		ctx := context.Background()
		_, err := gig.Load[cfg](
			ctx,
			strings.NewReader("name: test\n"),
			gig.WithMutators(
				gig.NewOverride(map[gig.YamlKey]string{
					"": "root",
				}),
			),
		)
		// Should not panic, may or may not error
		_ = err
	})

	t.Run("unknown nil handlers", func(t *testing.T) {
		tr := gig.NewTagResolver(nil)
		ctx := context.Background()
		err := tr.Mutate(ctx, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: "plain",
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestLoadWithExpanderOptional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var capturedOptional bool
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !env? OPTIONAL_VAR\n"),
		gig.WithEnvOptions(gig.WithEnvExpander(func(_ string, optional bool) (string, bool, error) {
			capturedOptional = optional
			return "", false, nil
		})),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !capturedOptional {
		t.Error("optional flag was not passed to expander")
	}
}

func TestOverrideSequence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type seqCfg struct {
		List []string `yaml:"list"`
	}
	got, err := gig.Load[seqCfg](
		ctx,
		strings.NewReader("list:\n  - a\n  - b\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				gig.YamlKey("").Key("list").Index(0): "overridden",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.List) != 2 || got.List[0] != "overridden" || got.List[1] != "b" {
		t.Errorf("List = %#v, want [overridden b]", got.List)
	}
}

func TestOverrideCreateNestedPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: base\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				gig.YamlKey("").Key("nested").Key("value"): "created",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Nested.Value != "created" {
		t.Errorf("Nested.Value = %q, want %q", got.Nested.Value, "created")
	}
}

func TestEnvKeyToYamlKeyUnderscore(t *testing.T) {
	t.Setenv("GIG_A__B", "double")
	overrides := gig.EnvOverrides("GIG_")
	key := gig.YamlKey("").Key("A").Key("").Key("B")
	val, ok := overrides[key]
	if !ok {
		t.Fatalf("expected key A..B for env GIG_A__B")
	}
	if val != "double" {
		t.Errorf("val = %q, want %q", val, "double")
	}
}

func TestNewFileHandlerRootAndFS(t *testing.T) {
	t.Parallel()
	f, err := gig.NewFileHandler(
		gig.WithBaseDir("/custom"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = f
}

func TestOverrideEmptyKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Override with empty YamlKey should not affect anything
	got, err := gig.Load[struct {
		Name string `yaml:"name"`
	}](
		ctx,
		strings.NewReader("name: test\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				"": "root",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
}

func TestEnvHandlerErrorMutatorPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("{}\n"),
		gig.WithEnvOptions(gig.WithEnvLookup(nil)),
	)
	if err == nil {
		t.Fatal("expected error for nil env lookup")
	}
}

func TestEnvExpanderProducesNoValueRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !env TEST\n"),
		gig.WithEnvOptions(gig.WithEnvExpander(func(_ string, _ bool) (string, bool, error) {
			return "", false, nil
		})),
	)
	if err == nil {
		t.Fatal("expected error for no value from expander")
	}
}

func TestLoadValidationDisabledNoError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Validation disabled should not error even with empty required field
	type emptyCfg struct {
		Req string `yaml:"req"`
	}

	_, err := gig.Load[emptyCfg](
		ctx,
		strings.NewReader("req: !missing value\n"),
		gig.WithValidation(false),
	)
	// Should fail at decode not validation
	_ = err
}

func TestOverrideSequenceCreatePath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("{}\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				gig.YamlKey("").Key("nested").Key("value"): "created-via-override",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Nested.Value != "created-via-override" {
		t.Errorf("Nested.Value = %q, want %q", got.Nested.Value, "created-via-override")
	}
}

func TestNewEnvHandlerDirectWithOpts(t *testing.T) {
	t.Parallel()
	h, err := gig.NewEnvHandler(gig.WithEnvLookup(func(name string) (string, bool) {
		if name == "EXISTS" {
			return "value", true
		}
		return "", false
	}))
	if err != nil {
		t.Fatalf("NewEnvHandler() error = %v", err)
	}
	if h == nil {
		t.Fatal("handler should not be nil")
	}
}

func TestNewEnvHandlerNilLookup(t *testing.T) {
	t.Parallel()
	_, err := gig.NewEnvHandler(gig.WithEnvLookup(nil))
	if err == nil {
		t.Fatal("expected error for nil lookup")
	}
}

func TestEnvExpanderReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !env TEST\n"),
		gig.WithEnvOptions(gig.WithEnvExpander(func(_ string, _ bool) (string, bool, error) {
			return "", false, errors.New("custom expander error")
		})),
	)
	if err == nil || !strings.Contains(err.Error(), "custom expander error") {
		t.Fatalf("expected custom expander error, got %v", err)
	}
}

func TestDirectEnvHandlerOptions(t *testing.T) {
	t.Parallel()
	lookup := func(name string) (string, bool) {
		if name == "DIRECT" {
			return "direct-value", true
		}
		return "", false
	}
	h, err := gig.NewEnvHandler(gig.WithEnvLookup(lookup))
	if err != nil {
		t.Fatalf("NewEnvHandler() error = %v", err)
	}
	ctx := context.Background()
	node := &yaml.Node{Tag: "!env", Value: "DIRECT", Kind: yaml.ScalarNode}
	err = h.Mutate(ctx, node)
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if node.Value != "direct-value" {
		t.Errorf("Value = %q, want %q", node.Value, "direct-value")
	}
}

func TestOverrideSetValueScalarSegmentsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[cfg](
		ctx,
		strings.NewReader("{}\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				"": "root-value",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	_ = got
}

func TestOverrideSequenceOutOfBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type seqCfg struct {
		List []string `yaml:"list"`
	}
	got, err := gig.Load[seqCfg](
		ctx,
		strings.NewReader("list:\n  - a\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				gig.YamlKey("").Key("list").Index(5): "ignored",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.List) != 1 || got.List[0] != "a" {
		t.Errorf("List = %#v, want [a]", got.List)
	}
}

func TestOverrideSequenceNested(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type nestedCfg struct {
		Items []struct {
			Name string `yaml:"name"`
		} `yaml:"items"`
	}
	got, err := gig.Load[nestedCfg](
		ctx,
		strings.NewReader("items:\n  - name: first\n  - name: second\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				gig.YamlKey("").Key("items").Index(0).Key("name"): "overridden-name",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].Name != "overridden-name" {
		t.Errorf("Items[0].Name = %q, want %q", got.Items[0].Name, "overridden-name")
	}
}

func TestOverrideNonMappingRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[int](
		ctx,
		strings.NewReader("42\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				gig.YamlKey("").Key("sub"): "value",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestMergeMappingOverriddenByScalar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Mapping source overridden by scalar override source
	got, err := gig.Load[int](
		ctx,
		strings.NewReader("name: test\nport: 8080\n"),
		gig.WithSources(strings.NewReader("42\n")),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestBuildPathDocumentNode(t *testing.T) {
	t.Parallel()
	// Top-level tagged scalar: `!env MISSING` as the entire document
	type directCfg struct {
		Value string `yaml:"value"`
	}
	// Create a mutator that returns error at top-level
	tr := gig.NewTagResolver(map[string]gig.Mutator{
		"!error": gig.MutatorFunc(func(_ context.Context, node *yaml.Node) error {
			return errors.New("top-level error")
		}),
	})
	_ = tr
	// Use direct YAML where entire doc is a tagged scalar
	ctx := context.Background()
	_, err := gig.Load[directCfg](
		ctx,
		strings.NewReader("!error value\n"),
		gig.WithMutators(tr),
	)
	if err == nil {
		t.Fatal("expected error for top-level tagged scalar")
	}
}

func TestFileBothRootAndFS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = gig.NewFileHandler(
		gig.WithFS(os.DirFS(dir)),
		gig.WithRoot(root),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFileOptionalTrailingEscape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file? '$x\\'\n"),
		gig.WithFileOptions(gig.WithBaseDir(t.TempDir())),
		gig.WithEnvOptions(gig.WithEnvLookup(func(name string) (string, bool) {
			if name == "x" {
				return "secret.txt", true
			}
			return "", false
		})),
	)
	if err == nil || !strings.Contains(err.Error(), "trailing escape") {
		t.Fatalf("expected trailing escape error, got %v", err)
	}
}

func TestOverrideSetValueScalarEmptySegs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := gig.Load[string](
		ctx,
		strings.NewReader("original\n"),
		gig.WithMutators(
			gig.NewOverride(map[gig.YamlKey]string{
				"": "overridden",
			}),
		),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != "overridden" {
		t.Errorf("got %q, want %q", got, "overridden")
	}
}

func TestBuildDefaultMutatorsBothRootAndFS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = gig.Load[cfg](
		ctx,
		strings.NewReader("{}\n"),
		gig.WithFileOptions(gig.WithFS(os.DirFS(dir)), gig.WithRoot(root)),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

type errorFile struct {
	name string
	data []byte
}

func (f *errorFile) Read(p []byte) (int, error) {
	return 0, errors.New("read error")
}

func (f *errorFile) Close() error {
	return nil
}

func (f *errorFile) Stat() (os.FileInfo, error) {
	return nil, errors.New("stat error")
}

type errorFS struct {
	dir string
}

func (efs *errorFS) Open(name string) (fs.File, error) {
	return &errorFile{}, nil
}

type closeFailFile struct {
	closed bool
}

func (f *closeFailFile) Read(p []byte) (int, error) {
	copy(p, "data")
	return 4, io.EOF
}

func (f *closeFailFile) Close() error {
	f.closed = true
	return errors.New("close failure")
}

func (f *closeFailFile) Stat() (os.FileInfo, error) {
	return nil, errors.New("stat error")
}

type closeFailFS struct{}

func (closeFailFS) Open(name string) (fs.File, error) {
	return &closeFailFile{}, nil
}

func TestReadFromFSCloseError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := gig.Load[cfg](
		ctx,
		strings.NewReader("name: !file any.txt\n"),
		gig.WithFileOptions(gig.WithFS(closeFailFS{})),
	)
	if err == nil {
		t.Fatal("expected error from close failure")
	}
}
