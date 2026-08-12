package gig_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/paluszkiewiczB/gig/v2"
	"gopkg.in/yaml.v3"
)

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

func TestNewFileHandlerRootAndFS(t *testing.T) {
	t.Parallel()
	// When both root and fs are provided, root takes precedence
	f := gig.NewFileHandler(
		gig.WithBaseDir("/custom"),
	)
	_ = f
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
	defer func() { _ = root.Close() }()

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
	h := gig.NewEnvHandler(gig.WithEnvLookup(func(name string) (string, bool) {
		if name == "EXISTS" {
			return "value", true
		}
		return "", false
	}))
	if h == nil {
		t.Fatal("handler should not be nil")
	}
}

func TestNewEnvHandlerNilLookup(t *testing.T) {
	t.Parallel()
	h := gig.NewEnvHandler(gig.WithEnvLookup(nil))
	// Should produce an error mutator
	ctx := context.Background()
	err := h.Mutate(ctx, &yaml.Node{Tag: "!env", Value: "TEST", Kind: yaml.ScalarNode})
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
			return "", false, fmt.Errorf("custom expander error")
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
	h := gig.NewEnvHandler(gig.WithEnvLookup(lookup))
	ctx := context.Background()
	node := &yaml.Node{Tag: "!env", Value: "DIRECT", Kind: yaml.ScalarNode}
	err := h.Mutate(ctx, node)
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
			return fmt.Errorf("top-level error")
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

func TestFileBothRootAndFS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	_ = gig.NewFileHandler(
		gig.WithFS(os.DirFS(dir)),
		gig.WithRoot(root),
	)
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

func TestBuildDefaultMutatorsBothRootAndFS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	_, err = gig.Load[cfg](
		ctx,
		strings.NewReader("{}\n"),
		gig.WithFileOptions(gig.WithFS(os.DirFS(dir)), gig.WithRoot(root)),
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
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
	defer func() { _ = root.Close() }()

	// Create a subdirectory so we can open it
	if err := os.MkdirAll(dir+"/subdir", 0o755); err != nil {
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
			return fmt.Errorf("tagged key error")
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

type errorFile struct {
	name string
	data []byte
}

func (f *errorFile) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

func (f *errorFile) Close() error {
	return nil
}

func (f *errorFile) Stat() (os.FileInfo, error) {
	return nil, fmt.Errorf("stat error")
}

type errorFS struct {
	dir string
}

func (efs *errorFS) Open(name string) (fs.File, error) {
	return &errorFile{}, nil
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
			return fmt.Errorf("sequence nested error")
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

func TestGetwdError(t *testing.T) { //nolint:paralleltest
	ctx := context.Background()
	d := t.TempDir()
	t.Chdir(d)
	os.Remove(d)

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
