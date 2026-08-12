# Gig v2 — Design Rewrite

## Shortcomings in v1

### 1. Rigid processing pipeline

v1 has three hard-coded phases that run in a fixed order:

```
merge optional → resolve required → decode → validate
```

Users who want to inject custom behavior (env overrides, custom resolvers, tree transformations) have no way to control *when* their logic runs or *what* the pipeline looks like. Everything is locked behind a small set of `With*` options.

### 2. `Load()` is a dumping ground for concerns

The current `Load[T]` accepts 9 options (and growing):

```
WithBaseDir, WithEnvLookup, WithEnvExpander, WithFS, WithRoot,
WithSources, WithResolver, WithContext, WithValidation
```

Mix of infrastructure (context, filesystem), tag-specific (env lookup, env expander), and pipeline (validation). Adding any new feature means adding another `With*` option, bloating the API.

### 3. Custom resolvers have no ordering

`WithResolver("!vault", fn)` registers a handler, but it always runs at the same phase as built-in `!env`/`!file` resolvers. Users cannot run a custom resolver *before* `!env` resolution, or *after* a transformation, or *instead of* the defaults.

### 4. Env-based config overrides require `!env` tags in YAML

To override a config value from an environment variable, the user must annotate every field in their YAML with `!env MY_VAR`. There is no way to bulk-override arbitrary keys from env vars without touching the YAML.

### 5. Global dependency on `os.Environ()` / `os.LookupEnv`

The env lookup is baked into `Load` via default `os.LookupEnv`. Testing with different env values requires `t.Setenv()` (mutating global state) or `WithEnvLookup` (which only affects `!env` resolution, not any new override mechanism).

### 6. Resolver / EnvLookup / EnvExpander are separate concepts

Three types where one would do. A resolver is just a function that mutates a `*yaml.Node`. The proliferation of types inflates the API surface.

---

## New Approach

### Everything is a `Mutator`

The pipeline is a flat chain of `Mutator` values, each receiving a `*yaml.Node` per YAML source:

```
For each source in order:
  unmarshal → run all Mutators in order → merge
Decode → validate
```

There are no phases. `DefaultMutators()` returns one `TagResolver` that handles all tagged nodes. Users replace, reorder, or extend the chain freely.

### `Load()` has 5 options (was 9)

| Option | Purpose |
|--------|---------|
| `WithMutators(...Mutator)` | Replace entire chain |
| `WithSources(...io.Reader)` | YAML layering |
| `WithValidation(bool)` | Post-decode validation |
| `WithFileOptions(...FileOption)` | Configure default `!file` handler (ignored if `WithMutators` used) |
| `WithEnvOptions(...EnvOption)` | Configure default `!env` handler (ignored if `WithMutators` used) |

`WithFileOptions` / `WithEnvOptions` exist purely for ergonomics — the common case of "just change the base directory" doesn't require rebuilding the entire `TagResolver` map.

### Option naming

`LoadOption` instead of `Option` to distinguish from option types on handler factories (`EnvOption`, `FileOption`).

### Tag resolution is one `TagResolver` struct

```go
NewTagResolver(map[string]Mutator{
    "!env":   NewEnvHandler(),
    "!env?":  NewEnvHandler(),   // same handler; checks node.Tag at runtime
    "!file":  NewFileHandler(),
    "!file?": NewFileHandler(),
})
```

The `?` suffix is **not** handled by the framework — no auto-stripping, no implicit registration. Users register each tag variant explicitly. The handler for `!env?` returns `ErrOptionalUnset` when the value is absent, signaling the walker to remove the field from the parent mapping. The walker does NOT inspect the tag name — it only acts on the returned error.

For custom resolvers:

```go
NewTagResolver(map[string]Mutator{
    "!vault":  vaultHandler,              // required: errors on failure
    "!vault?": vaultOptionalHandler,      // optional: returns ErrOptionalUnset
})
```

### Context is a parameter, not an option

`Load[T](ctx, src, ...opts)` — every `Mutator` and tag handler receives the same context.

### Env overrides are a first-class mutator

```go
NewOverride(EnvOverrides("GIG_"))
```

Bulk-override any config key from environment variables without YAML tags. The path supports dots (`GIG_address.street`) and bracket notation (`GIG_foo[0][a.b]`).

`EnvOverrides` returns `map[YamlKey]string` with pre-parsed keys. `NewOverride` uses `.Segments()` on each key to navigate the tree.

### YamlKey is structured and comparable

```go
type YamlKey string

func (k YamlKey) Key(name string) YamlKey   // appends ".name"
func (k YamlKey) Index(idx int) YamlKey      // appends "[idx]"
func (k YamlKey) Segments() []segment        // parse on demand
```

Since `YamlKey` is a `string` underneath, it is comparable and safe to use as a map key. The builder methods construct the canonical string form:

```go
YamlKey("foo").Index(0).Key("a.b")           // "foo[0][a.b]"
YamlKey("foo").Key("0").Key("a").Key("b")   // "foo.0.a.b"
```

`ParseYamlKey(s string) (YamlKey, []segment, error)` returns both the canonical key and pre-computed segments for callers who already have a string path.

### Default handlers

```go
func DefaultEnvHandler() Mutator   // NewEnvHandler() with os.LookupEnv
func DefaultFileHandler() Mutator  // NewFileHandler() with system filesystem
```

Exported zero-config factories so users building custom chains don't need to know the constructor options to replicate defaults.

---

## API Surface

```
// Core
type Mutator interface { Mutate(ctx, *yaml.Node) error }
type MutatorFunc func(ctx, *yaml.Node) error
type LoadOption func(*loader) error

func Load[T any](ctx, io.Reader, ...LoadOption) (T, error)
func WithMutators(m ...Mutator) LoadOption
func WithSources(r ...io.Reader) LoadOption
func WithValidation(ok bool) LoadOption
func WithFileOptions(...FileOption) LoadOption
func WithEnvOptions(...EnvOption) LoadOption

// YamlKey — structured YAML path, comparable (map-key safe)
type segment struct { key string; isIndex bool }
type YamlKey string
func (k YamlKey) Key(string) YamlKey
func (k YamlKey) Index(int) YamlKey
func (k YamlKey) Segments() []segment
func ParseYamlKey(string) (YamlKey, []segment, error)

// TagResolver — single mutator for all tag dispatching
type TagResolver struct { ... }
func NewTagResolver(map[string]Mutator) *TagResolver
func (tr *TagResolver) Handle(tag string, handler Mutator) *TagResolver
func (tr *TagResolver) Mutate(ctx, *yaml.Node) error

// Handler factories (return Mutator, for use inside NewTagResolver)
type EnvLookup func(name string) (value string, set bool)
type EnvExpander func(expression string, optional bool) (string, bool, error)

func NewEnvHandler(opts ...EnvOption) Mutator
type EnvOption func(*envConfig)
func WithEnvLookup(EnvLookup) EnvOption
func WithEnvExpander(EnvExpander) EnvOption

func NewFileHandler(opts ...FileOption) Mutator
type FileOption func(*fileConfig)
func WithBaseDir(string) FileOption
func WithFS(fs.FS) FileOption
func WithRoot(*os.Root) FileOption

// Default handlers (zero-config factories)
func DefaultEnvHandler() Mutator
func DefaultFileHandler() Mutator

// Env override (first-class mutator)
func NewOverride(map[YamlKey]string) Mutator
func EnvOverrides(prefix string) map[YamlKey]string
var ErrOptionalUnset error

// Defaults
func DefaultMutators() []Mutator

// Validation (unchanged from v1)
type Validator interface { Validate() error }
type ValidatorContext interface { ValidateContext(context.Context) error }
type ResolveError struct { Path string; Err error }
```

---

## Usage examples

**Default (no customization):**
```go
cfg, err := gig.Load[Config](ctx, os.Stdin)
```

**Override config keys from environment variables:**
```go
cfg, err := gig.Load[Config](ctx, yamlFile,
    gig.WithMutators(
        gig.NewOverride(gig.EnvOverrides("GIG_")),
        gig.DefaultMutators()...,
    ),
)
```

**Custom vault resolver, reorder (override before resolution):**
```go
cfg, err := gig.Load[Config](ctx, yamlFile,
    gig.WithMutators(
        gig.NewOverride(gig.EnvOverrides("GIG_")),
        gig.NewTagResolver(map[string]gig.Mutator{
            "!env":   gig.DefaultEnvHandler(),
            "!env?":  gig.DefaultEnvHandler(),
            "!file":  gig.DefaultFileHandler(),
            "!file?": gig.DefaultFileHandler(),
            "!vault": vaultHandler,
            "!vault?": vaultOptionalHandler,
        }),
    ),
)
```

**Test without global state:**
```go
cfg, err := gig.Load[Config](ctx, yamlFile,
    gig.WithMutators(
        gig.NewOverride(map[gig.YamlKey]string{
            gig.YamlKey("").Key("name"): "test",
        }),
    ),
)
```

**Bracket notation for keys containing dots:**
```go
gig.NewOverride(map[gig.YamlKey]string{
    gig.YamlKey("").Key("foo").Index(0).Key("a.b"): "value",
})
```

**Just change the file base directory (without rebuilding the resolver map):**
```go
cfg, err := gig.Load[Config](ctx, yamlFile,
    gig.WithFileOptions(gig.WithBaseDir("/etc/myapp")),
)
```

---

## Pros

| Pro | Explanation |
|-----|-------------|
| **Flat pipeline** | No phases, no magic ordering. Mutators run in registration order. |
| **Full control** | Replace, reorder, add, or remove any mutator. |
| **Small `Load` API** | 5 focused options instead of 9. |
| **Self-contained mutators** | Each mutator owns its configuration. No cross-contamination on `Load`. |
| **Env overrides without YAML** | `NewOverride(EnvOverrides(...))` — no `!env` tags needed. |
| **Injectable env source** | `EnvOverrides()` calls `os.Environ()` but returns a pure `map`; test with a literal map. |
| **Single tag-dispatching walk** | One `TagResolver` walks the tree once for all tags, not once per registered tag. |
| **No global state** | No default `os.LookupEnv` baked into Load. Everything is explicitly constructed. |
| **No `?` magic** | Optionality is a handler concern, not a framework concern. Tag handlers opt in by returning `ErrOptionalUnset`. |
| **Ergonomics** | `WithFileOptions` / `WithEnvOptions` cover the most common customizations without rebuilding the resolver map. |
| **Structured YamlKey** | Builder API (`YamlKey("foo").Index(0).Key("a.b")`) is type-safe and unambiguous. Comparable as map key. |

## Cons

| Con | Explanation |
|-----|-------------|
| **More verbose for simple cases** | You need to understand `Mutator`, `TagResolver`, `NewTagResolver` even if you just want the defaults. Mitigated by `DefaultMutators()`. |
| **Breaking change** | v2 is not backward-compatible with v1. Users must rewrite their `Load` calls. |
| **Mutator chain complexity** | Users can accidentally break resolution by omitting built-in handlers from `NewTagResolver`. |
| **No compile-time safety** | The `map[string]Mutator` in `NewTagResolver` is a runtime map — misspelling `"!env"` as `"!En"` compiles but fails silently at runtime. |
| **Duplicate registration** | Each `?` variant must be registered explicitly, even when the handler is the same function. |

---

## Migration Path (v1 → v2)

| v1 | v2 |
|----|----|
| `gig.Load[Config](reader)` | `gig.Load[Config](ctx, reader)` |
| `gig.Load[Config](reader, gig.WithEnvLookup(fn))` | `gig.Load[Config](ctx, reader, gig.WithEnvOptions(gig.WithEnvLookup(fn)))` |
| `gig.Load[Config](reader, gig.WithResolver("!vault", fn))` | `gig.Load[Config](ctx, reader, gig.WithMutators(append(gig.DefaultMutators(), tagMutator("!vault", fn))...))` |
| N/A | `gig.Load[Config](ctx, reader, gig.WithMutators(gig.NewOverride(gig.EnvOverrides("GIG_")), ...))` |
| `gig.Load[Config](reader, gig.WithContext(ctx))` | `gig.Load[Config](ctx, reader)` |
| `gig.Load[Config](reader, gig.WithBaseDir(d))` | `gig.Load[Config](ctx, reader, gig.WithFileOptions(gig.WithBaseDir(d)))` |
