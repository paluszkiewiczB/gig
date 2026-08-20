// Package gig loads typed configuration from YAML with environment
// variables, file references, and a flat pipeline of Mutators.
//
// # Quick Start
//
//	type Config struct {
//	    Login    string `yaml:"login"`
//	    Password string `yaml:"password"`
//	}
//
//	cfg, err := gig.Load[Config](ctx, strings.NewReader(`
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
//	cfg, err := gig.Load[Config](ctx, yamlFile, gig.WithMutators(
//	    gig.NewTagResolver(map[string]gig.Mutator{
//	        "!vault": vaultHandler,
//	    }),
//	))
//
// # Validation
//
// Implement Validator or ValidatorContext on your config type.  Load
// calls Validate() after unmarshaling.  WithValidation(false) to disable.
//
// # Layered Overrides
//
//	gig.Load[Config](ctx, base, gig.WithSources(override))
//
// Optional tags (!env?, !file?) leave a field unchanged when the value is
// missing, preserving a value from an earlier source.
//
// # Processing Order
//
//  1. For each source in order:
//     - read the source
//     - unmarshal YAML
//     - run all Mutators in order
//     - merge into the accumulator (maps combine, scalars and sequences replace)
//  2. Decode into T.
//  3. Validate if implemented.
//
// # Defaults
//
//   - Mutator chain: a TagResolver handling !env, !env?, !file, !file?.
//   - File base directory: the absolute current working directory.
//   - Env lookup: os.LookupEnv.
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
package gig
