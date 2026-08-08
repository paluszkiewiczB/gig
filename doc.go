// Package gig loads typed configuration from YAML with environment
// variables and file references.
//
// # Quick Start
//
//	type Config struct {
//	    Login    string `yaml:"login"`
//	    Password string `yaml:"password"`
//	}
//
//	cfg, err := gig.Load[Config](strings.NewReader(`
//	login:    !env '${LOGIN:-admin}'
//	password: !file /run/secrets/db_password
//	`))
//
// # Tags
//
//	!env  NAME       Required environment variable
//	!env? NAME       Optional environment variable
//	!file path       Required file contents (whitespace trimmed)
//	!file? path      Optional file contents (whitespace trimmed)
//
// # Environment Expressions
//
// !env supports Bash-style expansion:
//
//	$VAR              short form
//	${VAR}            full form
//	${VAR:-default}   default when VAR is unset or empty
//	${VAR-default}    default when VAR is unset only
//	${VAR:+alternate} alternate when VAR is set and non-empty
//	${VAR+alternate}  alternate when VAR is set
//	${VAR:?message}   error with message when VAR is unset or empty
//	${VAR?message}    error with message when VAR is unset only
//
// Nested expressions are supported:
//
//	message: !env '${LOG_LEVEL:-${ENV:-info}}'
//
// A backslash escapes the next character in fallback words, producing a
// literal character.  When GREETING is unset, \$-there- resolves to $there:
//
//	msg: !env '${GREETING:-hello \$there}'   ->   "hello $there"
//
// Assignment operators (= and :=) are rejected.
//
// File tags use the unrestricted system filesystem (os.DirFS("/")) by default.
// Use WithFS or WithRoot to restrict access (if both are provided, the last one wins).  Relative paths are
// resolved against the base directory (WithBaseDir).
//
// # Custom Resolvers
//
//	gig.WithResolver("!vault", func(ctx context.Context, node *yaml.Node) error {
//	    secret, err := vaultClient.GetSecret(ctx, node.Value)
//	    node.Tag = ""
//	    node.Value = secret
//	    return err
//	})
//
// To support optional resolution (the "?" suffix, as in !vault?), register
// the tag with the "?" included: WithResolver("!vault?", resolver).
//
// # Validation
//
// Implement Validator or ValidatorContext on your config type.  Load
// calls Validate() after unmarshaling.  WithValidation(false) to disable.
//
// # Layered Overrides
//
//	gig.Load[Config](base, gig.WithSources(override))
//
// Optional tags (!env?, !file?) leave a field unchanged when the value is
// missing, preserving a value from an earlier source.
//
// # Processing Order
//
//  1. For each source in order:
//     - read the source
//     - unmarshal YAML
//     - resolve optional tags (unset values remove that field)
//     - merge into the accumulator (maps combine, scalars and sequences replace)
//  2. Resolve required tags (!env, !file, custom) on the merged tree.
//  3. Decode into T.
//  4. Validate if implemented.
//
// # Defaults
//
//   - Base directory: the absolute current working directory (system filesystem)
//     or "." (configured filesystem).
//   - Loading context: context.Background().
//   - Environment lookup: os.Getenv (overridable with WithEnvLookup).
//   - Validation is enabled by default.  Use WithValidation(false) to disable.
//
// # Errors
//
// Resolution failures return ResolveError with paths like $.login.
// Use errors.As to extract the path from a failed load:
//
//	if resolveErr, ok := errors.As[ResolveError](err); ok {
//	    fmt.Println("path:", resolveErr.Path)
//	}
//
// WithEnvLookup and WithEnvExpander replace the default environment lookup
// and expression expander used by !env and !env?.
package gig
