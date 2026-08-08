package gig

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var errOptionalUnset = errors.New("optional value is unset")

func envTagResolver(expander EnvExpander) Resolver {
	return func(_ context.Context, node *yaml.Node) error {
		value, err := expander(node.Value, node.Tag == "!env?")
		if err != nil {
			return err
		}
		node.Tag = ""
		node.Value = value
		return nil
	}
}

func expandEnv(value string, optional bool, lookup EnvLookup) (string, error) {
	if !strings.HasPrefix(value, "${") {
		resolved, ok := lookup(value)
		if !ok {
			if optional {
				return "", errOptionalUnset
			}
			return "", fmt.Errorf("environment variable %s is not set", value)
		}
		return resolved, nil
	}

	parser := envParser{
		input:  value,
		lookup: lookup,
	}
	result, err := parser.parseExpression()
	if err != nil {
		return "", err
	}
	if parser.pos != len(parser.input) {
		return "", fmt.Errorf("unexpected text after environment expansion")
	}
	if !result.present && optional {
		return "", errOptionalUnset
	}
	return result.value, nil
}

func expandEnvWord(value string, lookup EnvLookup) (string, error) {
	parser := envParser{
		input:  value,
		lookup: lookup,
	}
	result, err := parser.evaluateWord(value)
	if err != nil {
		return "", err
	}
	return result.value, nil
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
		return envResult{}, fmt.Errorf("environment assignment operator %s is unsupported", operator)
	}

	word, err := p.readWord()
	if err != nil {
		return envResult{}, err
	}

	value, present := p.lookup(name)
	switch operator {
	case "-":
		if present {
			return envResult{value: value, present: true}, nil
		}
		return p.evaluateWord(word)
	case ":-":
		if present && value != "" {
			return envResult{value: value, present: true}, nil
		}
		return p.evaluateWord(word)
	case "+":
		if !present {
			return envResult{present: true}, nil
		}
		return p.evaluateWord(word)
	case ":+":
		if !present || value == "" {
			return envResult{present: true}, nil
		}
		return p.evaluateWord(word)
	case "?":
		if present {
			return envResult{value: value, present: true}, nil
		}
		return p.requiredError(name, word, false)
	case ":?":
		if present && value != "" {
			return envResult{value: value, present: true}, nil
		}
		return p.requiredError(name, word, true)
	default:
		return envResult{}, fmt.Errorf("unsupported environment operator %s", operator)
	}
}

func (p *envParser) parseName() (string, error) {
	if p.pos >= len(p.input) || !isEnvNameStart(p.input[p.pos]) {
		return "", fmt.Errorf("invalid environment variable name at position %d", p.pos)
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
		return "", fmt.Errorf("unterminated environment expansion")
	}
	if p.input[p.pos] == ':' {
		if p.pos+1 >= len(p.input) {
			return "", fmt.Errorf("environment operator is incomplete")
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
			if p.pos+1 >= len(p.input) {
				return "", fmt.Errorf("trailing escape in environment expansion")
			}
			if p.pos+2 < len(p.input) && p.input[p.pos:p.pos+3] == "\\${" {
				nested++
			} else if nested > 0 && p.input[p.pos+1] == '}' {
				nested--
			}
			word.WriteString(p.input[p.pos : p.pos+2])
			p.pos += 2
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

	return "", fmt.Errorf("unterminated environment expansion")
}

func (p *envParser) evaluateWord(word string) (envResult, error) {
	parser := envParser{input: word, lookup: p.lookup}
	var value strings.Builder

	for parser.pos < len(parser.input) {
		if parser.input[parser.pos] == '\\' {
			if parser.pos+1 >= len(parser.input) {
				return envResult{}, fmt.Errorf("trailing escape in environment word")
			}
			value.WriteByte(parser.input[parser.pos+1])
			parser.pos += 2
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
		if parser.input[parser.pos] == '$' && parser.pos+1 < len(parser.input) && isEnvNameStart(parser.input[parser.pos+1]) {
			parser.pos++
			name, _ := parser.parseName()
			resolved, _ := parser.lookup(name)
			value.WriteString(resolved)
			continue
		}
		value.WriteByte(parser.input[parser.pos])
		parser.pos++
	}

	return envResult{value: value.String(), present: true}, nil
}

func (p *envParser) requiredError(name, word string, empty bool) (envResult, error) {
	message, err := p.evaluateWord(word)
	if err != nil {
		return envResult{}, err
	}
	if message.value == "" {
		if empty {
			return envResult{}, fmt.Errorf("environment variable %s is empty", name)
		}
		return envResult{}, fmt.Errorf("environment variable %s is not set", name)
	}
	return envResult{}, errors.New(message.value)
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
