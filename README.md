# gig

Load typed configuration from YAML with environment variables, file references,
and a flat pipeline of `Mutator`s.

## Quick Start

```go
type Config struct {
    Login    string `yaml:"login"`
    Password string `yaml:"password"`
}

cfg, err := gig.Load[Config](ctx, strings.NewReader(`
login:    !env '${LOGIN:-admin}'
password: !file /run/secrets/db_password
`))
if err != nil {
    return err
}
```

`ctx` is passed to every `Mutator` and to `ValidatorContext`.

## Tags

| Tag | Description |
|---|---|
| `!env NAME` | Required environment variable |
| `!env? NAME` | Optional environment variable |
| `!file path` | Required file contents (whitespace trimmed) |
| `!file? path` | Optional file contents (whitespace trimmed) |

A tag with no registered handler is a resolution error.

## Environment Expressions

`!env` supports Bash-style expansion:

| Expression | When the value is used |
|---|---|
| `$VAR` | Short form, value of VAR, empty if unset |
| `${VAR}` | Full form |
| `${VAR:-default}` | VAR unset or empty  =  `default`, otherwise VAR |
| `${VAR-default}` | VAR unset  =  `default`, otherwise VAR |
| `${VAR:+alternate}` | VAR set and non-empty  =  `alternate`, otherwise `""` |
| `${VAR+alternate}` | VAR set  =  `alternate`, otherwise `""` |
| `${VAR:?message}` | VAR unset or empty  =  error with `message`, otherwise VAR |
| `${VAR?message}` | VAR unset  =  error with `message`, otherwise VAR |

Nested:

```yaml
LOG_LEVEL: !env '${LOG_LEVEL:-${ENV:-info}}'
```

A backslash escapes the next character in fallback words, producing a literal
character. When `GREETING` is unset, `\$` resolves to a literal `$`:

```yaml
msg: !env '${GREETING:-hello \$there}'    = "hello $there"
```

Assignment operators (`=`, `:=`) are rejected.

## Custom Resolvers

The pipeline is a chain of `Mutator`s, each receiving a `*yaml.Node`.
`NewTagResolver` dispatches tagged scalars to handlers:

```go
resolver := gig.NewTagResolver(map[string]gig.Mutator{
    "!env":   gig.DefaultEnvHandler(),
    "!env?":  gig.DefaultEnvHandler(),
    "!file":  gig.DefaultFileHandler(),
    "!file?": gig.DefaultFileHandler(),
    "!vault": gig.MutatorFunc(func(ctx context.Context, node *yaml.Node) error {
        secret, err := vaultClient.GetSecret(ctx, node.Value)
        if err != nil {
            return err
        }
        node.Tag = ""
        node.Value = secret
        return nil
    }),
})

cfg, err := gig.Load[Config](ctx, yamlFile, gig.WithMutators(resolver))
```

Use `gig.DefaultMutators()` to build on the default chain.

## Validation

Implement `Validator` or `ValidatorContext` on your config type.
`Load` calls it after unmarshaling. Use `WithValidation(false)` to disable.

## Layered Overrides

```go
cfg, err := gig.Load[Config](ctx, base, gig.WithSources(override))
```

Mapping values merge recursively; scalars and sequences replace earlier values.
Fields tagged with `!env?` or `!file?` keep their value from an earlier source
when the override doesn't provide them.

## Environment Overrides

Override arbitrary keys without touching the YAML. `EnvOverrides` reads
variables with a prefix, where `__` separates path segments and `_` separates
keys — with prefix `CFG_`, `CFG_database__host` maps to `database.host`.

```go
overrides, err := gig.NewOverride(gig.EnvOverrides("CFG_"))
if err != nil {
    return err
}

cfg, err := gig.Load[Config](ctx, yamlFile,
    gig.WithMutators(append([]gig.Mutator{overrides}, gig.DefaultMutators()...)...),
)
```

Keys can also be supplied directly. `NewOverride` returns an error if any key
is not a valid YAML path:

```go
overrides, err := gig.NewOverride(map[gig.YamlKey]string{
    gig.YamlKey("").Key("database").Key("host"): "localhost",
    gig.YamlKey("").Key("servers").Index(0):     "primary",
})
```

Each `Key` argument is one literal segment, so field names containing `.` or
`[` are supported. `gig.YamlKey("").Key("frameworks").Key(".net")` targets the
`.net` key, and `.Key("filters[0]")` targets the literal `filters[0]` key rather
than a sequence index (`Index(0)`).

## Processing Order

1. For each source in order: read it, unmarshal YAML, run all `Mutator`s in
   order, merge into the accumulator.
2. Decode into `T`.
3. Validate, if implemented.

## Defaults

- Mutator chain: a `TagResolver` handling `!env`, `!env?`, `!file`, `!file?`.
- Environment lookup: `os.LookupEnv` (override with `WithEnvOptions`).
- File base directory: the absolute current working directory (override with
  `WithFileOptions`).
- Validation: enabled (disable with `WithValidation(false)`).

## Errors

Resolution failures return `ResolveError` with the configuration path
(e.g., `$.database.host`). Extract it with `errors.As`:

```go
resolveErr, ok := errors.As[gig.ResolveError](err)
```

## Options

- [`WithEnvOptions`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithEnvOptions)
- [`WithFileOptions`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithFileOptions)
- [`WithMutators`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithMutators)
- [`WithSources`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithSources)
- [`WithValidation`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithValidation)

## Reference

- [`Mutator`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#Mutator)
- [`NewTagResolver`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#NewTagResolver)
- [`NewEnvHandler`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#NewEnvHandler)
- [`NewFileHandler`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#NewFileHandler)
- [`NewOverride`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#NewOverride)
- [`YamlKey`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#YamlKey)
- [`ResolveError`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#ResolveError)
- [`Validator`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#Validator)
- [`ValidatorContext`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#ValidatorContext)

## Install

```sh
go get github.com/paluszkiewiczB/gig
```
