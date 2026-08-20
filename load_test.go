package gig_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paluszkiewiczB/gig"
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
	t.Run("empty document", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](strings.NewReader("{}\n"))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "" || got.Port != 0 || got.Enabled || got.Nested.Value != "" {
			t.Errorf("got %+v, want zero value", got)
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](strings.NewReader("name: [\n"))
		if err == nil || !strings.Contains(err.Error(), "yaml:") {
			t.Fatalf("Load() error = %v, want yaml error", err)
		}
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()
		wantErr := errSourceFailed
		_, err := gig.Load[cfg](failReader{err: wantErr})
		if err == nil || !errors.Is(err, wantErr) {
			t.Fatalf("Load() error = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("decode type mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[int](strings.NewReader("name: foo\n"))
		if err == nil || !strings.Contains(err.Error(), "unmarshal") {
			t.Fatalf("Load() error = %v, want unmarshal error", err)
		}
	})

	t.Run("empty source", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](strings.NewReader(""))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	})
}

func TestLoadOverride(t *testing.T) {
	t.Parallel()
	t.Run("nested mapping merges", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](
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
		got, err := gig.Load[int](strings.NewReader("1\n"), gig.WithSources(strings.NewReader("2\n")))
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
	env := fakeEnv{
		"setEnv":   setValue,
		"emptyEnv": "",
		"otherEnv": otherValue,
	}
	load := func(src string, opts ...gig.Option) (cfg, error) {
		return gig.Load[cfg](strings.NewReader(src), append([]gig.Option{gig.WithEnvLookup(env.lookup)}, opts...)...)
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
	t.Run("required missing reports error", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](strings.NewReader("name: !file missing-secret\n"))
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
			strings.NewReader("name: !file secret.txt\n"),
			gig.WithBaseDir(dir),
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
			strings.NewReader("name: !file ${GIG_TEST_FILE:-${GIG_TEST_OTHER_FILE:-default.yaml}}\n"),
			gig.WithBaseDir(dir),
			gig.WithEnvLookup(func(_ string) (string, bool) { return "", false }),
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
		defer func() {
			if err := root.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		}()

		got, err := gig.Load[cfg](
			strings.NewReader("name: !file secret.txt\n"),
			gig.WithRoot(root),
		)
		if err != nil {
			t.Fatalf("happy: Load() error = %v", err)
		}
		if got.Name != "rooted-secret" {
			t.Errorf("Name = %q, want %q", got.Name, "rooted-secret")
		}

		_, err = gig.Load[cfg](
			strings.NewReader("name: !file ../outside.txt\n"),
			gig.WithRoot(root),
		)
		if err == nil || !strings.Contains(err.Error(), "invalid file path") {
			t.Fatalf("traversal: Load() error = %v, want invalid path error", err)
		}

		_, err = gig.Load[cfg](
			strings.NewReader("name: !file missing.txt\n"),
			gig.WithRoot(root),
		)
		if err == nil || !strings.Contains(err.Error(), "cannot read") {
			t.Fatalf("missing: Load() error = %v, want cannot read error", err)
		}
	})

	t.Run("optional", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		got, err := gig.Load[cfg](
			strings.NewReader("name: root\n"),
			gig.WithBaseDir(dir),
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
			strings.NewReader("name: root\n"),
			gig.WithBaseDir(dir),
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
			strings.NewReader("name: !file '$x\\'\n"),
			gig.WithBaseDir(dir),
			gig.WithEnvLookup(func(_ string) (string, bool) {
				return "secret.txt", true
			}),
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
			strings.NewReader("name: !file " + filePath + "\n"),
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
		got, err := gig.Load[cfg](
			strings.NewReader("name: !upper hello world\n"),
			gig.WithResolver("!upper", func(_ context.Context, node *yaml.Node) error {
				node.Tag = ""
				node.Value = strings.ToUpper(node.Value)

				return nil
			}),
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
		wantErr := errResolverFailed
		_, err := gig.Load[cfg](
			strings.NewReader("name: !broken value\n"),
			gig.WithResolver("!broken", func(_ context.Context, _ *yaml.Node) error {
				return wantErr
			}),
		)
		if err == nil {
			t.Fatal("Load() error = nil, want resolver error")
		}
		var resolveErr gig.ResolveError
		if !errors.As(err, &resolveErr) {
			t.Fatalf("error type = %T, want ResolveError", err)
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
		_, err := gig.Load[cfg](
			strings.NewReader("MY_VAR_2: !broken v\n"),
			gig.WithResolver("!broken", func(_ context.Context, _ *yaml.Node) error {
				return errResolverFailed
			}),
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
			strings.NewReader("name: !ctx hello\n"),
			gig.WithContext(ctx),
			gig.WithResolver("!ctx", func(c context.Context, node *yaml.Node) error {
				node.Tag = ""
				v, ok := c.Value(ctxKey{}).(string)
				if !ok {
					return fmt.Errorf("context value %T is not a string", c.Value(ctxKey{}))
				}
				node.Value = node.Value + "-" + v

				return nil
			}),
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
		got, err := gig.Load[cfg](
			strings.NewReader("name: !env GIG_TEST_OVERRIDE\n"),
			gig.WithEnvLookup(func(_ string) (string, bool) { return "original", true }),
			gig.WithResolver("!env", func(_ context.Context, node *yaml.Node) error {
				node.Tag = ""
				node.Value = "overridden-" + node.Value

				return nil
			}),
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
		_, err := gig.Load[cfg](strings.NewReader("{}\n"), gig.WithEnvLookup(nil))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("nil env expander", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](strings.NewReader("{}\n"), gig.WithEnvExpander(nil))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("nil root", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](strings.NewReader("{}\n"), gig.WithRoot(nil))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("nil file system", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](strings.NewReader("{}\n"), gig.WithFS(nil))
		if err == nil || !strings.Contains(err.Error(), "must not be nil") {
			t.Fatalf("Load() error = %v, want nil guard error", err)
		}
	})

	t.Run("with file system", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("fs-data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := gig.Load[cfg](
			strings.NewReader("name: !file secret.txt\n"),
			gig.WithFS(os.DirFS(dir)),
		)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.Name != "fs-data" {
			t.Errorf("Name = %q, want %q", got.Name, "fs-data")
		}

		_, err = gig.Load[cfg](
			strings.NewReader("name: !file /etc/passwd\n"),
			gig.WithFS(os.DirFS(dir)),
		)
		if err == nil || !strings.Contains(err.Error(), "absolute file path") {
			t.Fatalf("Load() error = %v, want absolute path error", err)
		}
	})

	t.Run("optional resolver error", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[cfg](
			strings.NewReader("name: base\n"),
			gig.WithSources(strings.NewReader("name: !err?\n")),
			gig.WithResolver("!err?", func(_ context.Context, _ *yaml.Node) error {
				return errBoom
			}),
		)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("Load() error = %v, want boom error", err)
		}
	})

	t.Run("unknown optional tag", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](
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
		type listCfg struct {
			List []string `yaml:"list"`
		}
		got, err := gig.Load[listCfg](
			strings.NewReader("list:\n  - !env GIG_SEQ_VAR\n  - literal\n"),
			gig.WithEnvLookup(func(_ string) (string, bool) { return "from-env", true }),
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
		type listCfg struct {
			List []string `yaml:"list"`
		}
		_, err := gig.Load[listCfg](
			strings.NewReader("list:\n  - !env GIG_MISSING_SEQ\n"),
		)
		if err == nil {
			t.Fatal("Load() error = nil, want missing env error")
		}
	})

	t.Run("sequence optional removes item", func(t *testing.T) {
		t.Parallel()
		type listCfg struct {
			List []string `yaml:"list"`
		}
		got, err := gig.Load[listCfg](
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
		type listCfg struct {
			List []string `yaml:"list"`
		}
		_, err := gig.Load[listCfg](
			strings.NewReader("list:\n  - ok\n"),
			gig.WithSources(strings.NewReader("list:\n  - !boom?\n")),
			gig.WithResolver("!boom?", func(_ context.Context, _ *yaml.Node) error {
				return errBang
			}),
		)
		if err == nil || !strings.Contains(err.Error(), "bang") {
			t.Fatalf("Load() error = %v, want bang error", err)
		}
	})

	t.Run("empty key identifier", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[map[string]string](
			strings.NewReader(`"": from-empty-key` + "\n"),
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
		type dotCfg struct {
			Field string `yaml:"field.with.dots"`
		}
		got, err := gig.Load[dotCfg](
			strings.NewReader(`"field.with.dots": !env GIG_DOT_VAR`+"\n"),
			gig.WithEnvLookup(func(_ string) (string, bool) { return "dot-value", true }),
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
		type aliasCfg struct {
			Name  string `yaml:"name"`
			Alias string `yaml:"alias"`
		}
		got, err := gig.Load[aliasCfg](
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
		d := t.TempDir()
		t.Chdir(d)
		if err := os.Remove(d); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}

		_, err := gig.Load[cfg](strings.NewReader("{}\n"))
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
	})
}

func TestLoadEnvLookup(t *testing.T) {
	t.Parallel()
	t.Run("custom lookup for env", func(t *testing.T) {
		t.Parallel()
		got, err := gig.Load[cfg](
			strings.NewReader("name: !env CUSTOM_NAME\n"),
			gig.WithEnvLookup(func(name string) (string, bool) {
				if name == "CUSTOM_NAME" {
					return "from-custom-lookup", true
				}

				return "", false
			}),
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
			strings.NewReader("name: !file $CUSTOM_FILE\n"),
			gig.WithBaseDir(dir),
			gig.WithEnvLookup(func(name string) (string, bool) {
				if name == "CUSTOM_FILE" {
					return "secret.txt", true
				}

				return "", false
			}),
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
			strings.NewReader("name: !env? CUSTOM_EXPRESSION\n"),
			gig.WithEnvExpander(func(expression string, optional bool) (string, bool, error) {
				gotOptional = optional
				if expression != "CUSTOM_EXPRESSION" {
					return "", false, fmt.Errorf("unexpected expression %q", expression)
				}

				return "from-custom-expander", true, nil
			}),
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
			strings.NewReader("name: !env ANY_NAME\n"),
			gig.WithEnvExpander(func(_ string, _ bool) (string, bool, error) {
				return "", false, nil
			}),
		)
		if err == nil || !strings.Contains(err.Error(), "produced no value") {
			t.Fatalf("Load() error = %v, want 'produced no value'", err)
		}
	})
}

func TestLoadValidation(t *testing.T) {
	t.Parallel()
	t.Run("value validator", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[valValid](strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[valValid](strings.NewReader(`name: ""` + "\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("pointer validator", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[ptrValid](strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[ptrValid](strings.NewReader(`name: ""` + "\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("value validator context", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[valValidCtx](strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[valValidCtx](strings.NewReader(`name: ""` + "\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("pointer validator context", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[ptrValidCtx](strings.NewReader("name: ok\n"))
		if err != nil {
			t.Fatalf("valid: Load() error = %v", err)
		}
		_, err = gig.Load[ptrValidCtx](strings.NewReader(`name: ""` + "\n"))
		if err == nil {
			t.Fatal("invalid: Load() error = nil")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		_, err := gig.Load[valValid](strings.NewReader(`name: ""`+"\n"), gig.WithValidation(false))
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
	})
}
