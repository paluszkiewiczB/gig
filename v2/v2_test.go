package gig_test

import (
	"context"
	"strings"
	"testing"

	"github.com/paluszkiewiczB/gig/v2"
	"gopkg.in/yaml.v3"
)

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

func TestNewFileHandlerNilRoot(t *testing.T) {
	t.Parallel()
	// This should not panic/crash - nil root is validated
	_ = gig.NewFileHandler(gig.WithRoot(nil))
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

func TestResolveErrorNilErr(t *testing.T) {
	t.Parallel()
	e := (&gig.ResolveError{Path: "$.test"}).Error()
	if e != "$.test" {
		t.Errorf("Error() = %q, want $.test", e)
	}
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
