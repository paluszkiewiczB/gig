// Package gig loads typed configuration from YAML with environment
// variables, file references, and a flat pipeline of Mutators.
//
// # Tags
//
//	!env  NAME       Required environment variable
//	!env? NAME       Optional environment variable
//	!file path       Required file contents (whitespace trimmed)
//	!file? path      Optional file contents (whitespace trimmed)
//
// A tag with no registered handler is a resolution error.
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
// Expressions may be nested. A backslash escapes the next character in a
// fallback word, producing a literal character. Assignment operators (= and :=)
// are rejected. See Example_expressions and Example_expressions_escape.
//
// # File Resolution
//
// !file reads through the system filesystem. Relative paths are resolved
// against the base directory, which defaults to the current working directory.
// Use WithFS or WithRoot to restrict access; if both are provided, WithRoot
// wins. See ExampleLoad_file.
//
// # Custom Resolvers
//
// Register a Mutator per tag with NewTagResolver. See ExampleWithMutators for a
// resolver built on MutatorFunc.
//
// # Validation
//
// Implement Validator or ValidatorContext on your config type. Load calls it
// after unmarshaling. Use WithValidation(false) to disable.
//
// # Layered Overrides
//
// Pass additional sources with WithSources. Mapping values merge recursively;
// scalars and sequences replace earlier values. Optional tags (!env?, !file?)
// leave a field unchanged when the value is missing, preserving a value from an
// earlier source. See ExampleWithSources.
//
// # Environment Overrides
//
// NewOverride replaces specific paths with literal strings, and EnvOverrides
// builds the path map from environment variables. With the prefix "CFG_", the
// variable CFG_database__host targets database.host: the prefix is stripped, __
// separates path segments, and _ separates keys. Keys can also be built
// directly with YamlKey, where each Key argument is one literal segment. See
// ExampleNewOverride and ExampleYamlKey.
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
//   - Validation is enabled by default. Use WithValidation(false) to disable.
//
// # Errors
//
// Resolution failures return ResolveError with paths like $.login. Extract the
// path with errors.AsType[*ResolveError]. See ExampleResolveError.
package gig
