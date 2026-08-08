# gig

Load typed configuration from YAML with environment variables and file references.

## Quick Start

```go
type Config struct {
    Login    string `yaml:"login"`
    Password string `yaml:"password"`
}

cfg, err := gig.Load[Config](strings.NewReader(`
login:    !env '${LOGIN:-admin}'
password: !file /run/secrets/db_password
`))
if err != nil {
    return err
}
```

## Tags

| Tag | Description |
|---|---|
| `!env NAME` | Required environment variable |
| `!env? NAME` | Optional environment variable |
| `!file path` | Required file contents (whitespace trimmed) |
| `!file? path` | Optional file contents (whitespace trimmed) |

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

A backslash escapes the next character in fallback words, producing a
literal character. When `GREETING` is unset, `\$` resolves to a literal `$`:

```yaml
msg: !env '${GREETING:-hello \$there}'    = "hello $there"
```

Assignment operators (`=`, `:=`) are rejected.

## Custom Resolvers

```go
gig.WithResolver("!vault", func(ctx context.Context, node *yaml.Node) error {
    secret, err := vaultClient.GetSecret(ctx, node.Value)
    node.Tag = ""
    node.Value = secret
    return err
})
```

## Validation

Implement `Validator` or `ValidatorContext` on your config type.
`Load` calls `Validate()` after unmarshaling.
Use `WithValidation(false)` to disable.

## Layered Overrides

```go
cfg, err := gig.Load[Config](base, gig.WithSources(override))
```

Fields tagged with `!env?` or `!file?` keep their value from an earlier
source when the override doesn't provide them.

## Defaults

- Environment lookup: `os.Getenv` (override with `WithEnvLookup`)
- Loading context: `context.Background()` (override with `WithContext`)
- Validation: enabled (disable with `WithValidation(false)`)

## Errors

Resolution failures return `ResolveError` with the configuration path
(e.g., `$.database.host`). Extract it with `errors.As`:

```go
resolveErr, ok := errors.As[gig.ResolveError](err)
```

## Options

- [`WithBaseDir`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithBaseDir)
- [`WithContext`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithContext)
- [`WithEnvExpander`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithEnvExpander)
- [`WithEnvLookup`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithEnvLookup)
- [`WithFS`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithFS)
- [`WithResolver`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithResolver)
- [`WithRoot`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithRoot)
- [`WithSources`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithSources)
- [`WithValidation`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#WithValidation)

## Reference

- [`ResolveError`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#ResolveError)
- [`Validator`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#Validator)
- [`ValidatorContext`](https://pkg.go.dev/github.com/paluszkiewiczB/gig#ValidatorContext)

## Install

```sh
go get github.com/paluszkiewiczB/gig
```
