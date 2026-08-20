package gig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvLookup looks up an environment variable. The boolean reports whether the
// variable is set, allowing callers to distinguish unset from empty.
type EnvLookup func(name string) (value string, set bool)

// EnvExpander resolves an environment expression. The optional argument is
// true for !env? and false for !env. When the expression is unset and
// optional is true, return ("", false, nil) to signal an absent value
// without producing an error.
type EnvExpander func(expression string, optional bool) (value string, present bool, err error)

// EnvOption configures a call to NewEnvHandler.
type EnvOption func(*envConfig) error

type envConfig struct {
	lookup   EnvLookup
	expander EnvExpander
}

// WithEnvLookup sets the lookup used by the default environment expander and
// by environment expansions inside !file and !file?.
func WithEnvLookup(lookup EnvLookup) EnvOption {
	return func(cfg *envConfig) error {
		if lookup == nil {
			return errors.New("gig: env lookup must not be nil")
		}
		cfg.lookup = lookup
		return nil
	}
}

// WithEnvExpander replaces the default Bash-like environment expression
// expander used by !env and !env?.
func WithEnvExpander(expander EnvExpander) EnvOption {
	return func(cfg *envConfig) error {
		if expander == nil {
			return errors.New("gig: env expander must not be nil")
		}
		cfg.expander = expander
		return nil
	}
}

// NewEnvHandler creates a Mutator that resolves !env and !env? tags using the
// given options. When called without options, it uses os.LookupEnv.
func NewEnvHandler(opts ...EnvOption) (Mutator, error) {
	cfg := &envConfig{
		lookup: os.LookupEnv,
	}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	return &envHandler{cfg: cfg}, nil
}

// DefaultEnvHandler returns a Mutator that resolves !env and !env? using
// os.LookupEnv without any custom configuration.
func DefaultEnvHandler() Mutator {
	return &envHandler{cfg: &envConfig{lookup: os.LookupEnv}}
}

type envHandler struct {
	cfg *envConfig
}

func (h *envHandler) Mutate(ctx context.Context, node *yaml.Node) error {
	value := node.Value
	optional := strings.HasSuffix(node.Tag, "?")

	if h.cfg.expander != nil {
		return h.mutateWithExpander(node, value, optional)
	}

	result, present, err := expandEnv(value, optional, h.cfg.lookup)
	if err != nil {
		return err
	}
	if !present {
		return ErrOptionalUnset
	}
	node.Tag = ""
	node.Value = result
	return nil
}

func (h *envHandler) mutateWithExpander(node *yaml.Node, value string, optional bool) error {
	result, present, err := h.cfg.expander(value, optional)
	if err != nil {
		return err
	}
	if !present {
		if optional {
			return ErrOptionalUnset
		}
		return fmt.Errorf("!env produced no value for %q", value)
	}
	node.Tag = ""
	node.Value = result
	return nil
}

var (
	errUnterminatedExpansion = errors.New("unterminated environment expansion")
	errNotSet                = errors.New("environment variable is not set")
)

func expandEnv(value string, optional bool, lookup EnvLookup) (string, bool, error) {
	if !strings.HasPrefix(value, "${") {
		resolved, ok := lookup(value)
		if !ok {
			if optional {
				return "", false, nil
			}
			return "", false, fmt.Errorf("%s: %w", value, errNotSet)
		}
		return resolved, true, nil
	}

	parser := envParser{
		input:  value,
		lookup: lookup,
		pos:    0,
	}
	result, err := parser.parseExpression()
	if err != nil {
		return "", false, err
	}
	if parser.pos != len(parser.input) {
		return "", false, errors.New("unexpected text after environment expansion")
	}
	if !result.present && optional {
		return "", false, nil
	}
	return result.value, true, nil
}

type envResult struct {
	value   string
	present bool
}

type envParser struct {
	input  string
	pos    int
	lookup func(string) (string, bool)
}

func (p *envParser) parseExpression() (envResult, error) {
	p.consume("${")
	return p.parseExpansionBody()
}

func (p *envParser) parseExpansionBody() (envResult, error) {
	name, err := p.parseName()
	if err != nil {
		return envResult{}, err
	}

	if p.consume("}") {
		value, present := p.lookup(name)
		return envResult{value: value, present: present}, nil
	}

	operator, err := p.parseOperator()
	if err != nil {
		return envResult{}, err
	}
	if operator == "=" || operator == ":=" {
		return envResult{}, fmt.Errorf("%s: environment assignment operator is unsupported", operator)
	}

	word, err := p.readWord()
	if err != nil {
		return envResult{}, err
	}

	return p.resolveOperator(name, operator, word)
}

func (p *envParser) resolveOperator(name, operator, word string) (envResult, error) {
	value, present := p.lookup(name)
	switch operator {
	case "-":
		return p.opDefault(value, present, word)
	case ":-":
		return p.opDefaultOrEmpty(value, present, word)
	case "+":
		return p.opAlternate(value, present, word)
	case ":+":
		return p.opAlternateOrEmpty(value, present, word)
	case "?":
		return p.opRequired(name, value, present, word, false)
	case ":?":
		return p.opRequired(name, value, present, word, true)
	default:
		return envResult{}, fmt.Errorf("%s: unsupported environment operator", operator)
	}
}

func (p *envParser) opDefault(value string, present bool, word string) (envResult, error) {
	if present {
		return envResult{value: value, present: true}, nil
	}
	return p.evaluateWord(word)
}

func (p *envParser) opDefaultOrEmpty(value string, present bool, word string) (envResult, error) {
	if present && value != "" {
		return envResult{value: value, present: true}, nil
	}
	return p.evaluateWord(word)
}

func (p *envParser) opAlternate(_ string, present bool, word string) (envResult, error) {
	if !present {
		return envResult{value: "", present: true}, nil
	}
	return p.evaluateWord(word)
}

func (p *envParser) opAlternateOrEmpty(value string, present bool, word string) (envResult, error) {
	if !present || value == "" {
		return envResult{value: "", present: true}, nil
	}
	return p.evaluateWord(word)
}

func (p *envParser) opRequired(name, value string, present bool, word string, empty bool) (envResult, error) {
	if empty && (!present || value == "") {
		return p.requiredError(name, word, true)
	}
	if !empty && !present {
		return p.requiredError(name, word, false)
	}
	return envResult{value: value, present: true}, nil
}

func (p *envParser) parseName() (string, error) {
	if p.pos >= len(p.input) || !isEnvNameStart(p.input[p.pos]) {
		return "", fmt.Errorf("%d: invalid environment variable name", p.pos)
	}
	start := p.pos
	p.pos++
	for p.pos < len(p.input) && isEnvNameChar(p.input[p.pos]) {
		p.pos++
	}
	return p.input[start:p.pos], nil
}

func (p *envParser) parseOperator() (string, error) {
	if p.pos >= len(p.input) {
		return "", errUnterminatedExpansion
	}
	if p.input[p.pos] == ':' {
		if p.pos+1 >= len(p.input) {
			return "", errors.New("environment operator is incomplete")
		}
		operator := p.input[p.pos : p.pos+2]
		p.pos += 2
		return operator, nil
	}
	operator := p.input[p.pos : p.pos+1]
	p.pos++
	return operator, nil
}

func (p *envParser) readWord() (string, error) {
	var word strings.Builder
	nested := 0

	for p.pos < len(p.input) {
		if p.input[p.pos] == '}' && nested == 0 {
			p.pos++
			return word.String(), nil
		}
		if p.input[p.pos] == '\\' {
			text, err := p.readWordEscape(&nested)
			if err != nil {
				return "", err
			}
			word.WriteString(text)
			continue
		}
		if p.consume("${") {
			nested++
			word.WriteString("${")
			continue
		}
		if p.input[p.pos] == '}' && nested > 0 {
			nested--
		}
		word.WriteByte(p.input[p.pos])
		p.pos++
	}

	return "", errUnterminatedExpansion
}

func (p *envParser) readWordEscape(nested *int) (string, error) {
	if p.pos+1 >= len(p.input) {
		return "", errors.New("trailing escape in environment expansion")
	}
	if p.pos+2 < len(p.input) && p.input[p.pos:p.pos+3] == "\\${" {
		*nested++
	} else if *nested > 0 && p.input[p.pos+1] == '}' {
		*nested--
	}
	seq := p.input[p.pos : p.pos+2]
	p.pos += 2
	return seq, nil
}

func (p *envParser) evaluateWord(word string) (envResult, error) {
	parser := envParser{input: word, lookup: p.lookup, pos: 0}
	var value strings.Builder

	for parser.pos < len(parser.input) {
		if parser.input[parser.pos] == '\\' {
			b, err := parser.readEscape()
			if err != nil {
				return envResult{}, err
			}
			value.WriteByte(b)
			continue
		}
		if parser.consume("${") {
			result, err := parser.parseExpansionBody()
			if err != nil {
				return envResult{}, err
			}
			value.WriteString(result.value)
			continue
		}
		if parser.handleSimpleName(&value) {
			continue
		}
		value.WriteByte(parser.input[parser.pos])
		parser.pos++
	}

	return envResult{value: value.String(), present: true}, nil
}

func (p *envParser) readEscape() (byte, error) {
	if p.pos+1 >= len(p.input) {
		return 0, errors.New("trailing escape in environment word")
	}
	b := p.input[p.pos+1]
	p.pos += 2
	return b, nil
}

func (p *envParser) handleSimpleName(value *strings.Builder) bool {
	if p.input[p.pos] != '$' {
		return false
	}
	if p.pos+1 >= len(p.input) || !isEnvNameStart(p.input[p.pos+1]) {
		return false
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.input) && isEnvNameChar(p.input[p.pos]) {
		p.pos++
	}
	resolved, _ := p.lookup(p.input[start:p.pos])
	value.WriteString(resolved)
	return true
}

func (p *envParser) requiredError(name, word string, empty bool) (envResult, error) {
	message, err := p.evaluateWord(word)
	if err != nil {
		return envResult{}, err
	}
	if message.value == "" {
		if empty {
			return envResult{}, fmt.Errorf("%s: environment variable is empty", name)
		}
		return envResult{}, fmt.Errorf("%s: %w", name, errNotSet)
	}
	return envResult{}, fmt.Errorf("%s", message.value)
}

func (p *envParser) consume(prefix string) bool {
	if p.pos+len(prefix) > len(p.input) || !strings.HasPrefix(p.input[p.pos:], prefix) {
		return false
	}
	p.pos += len(prefix)
	return true
}

func isEnvNameStart(char byte) bool {
	return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func isEnvNameChar(char byte) bool {
	return isEnvNameStart(char) || char >= '0' && char <= '9'
}

var _ Mutator = (*envHandler)(nil)
