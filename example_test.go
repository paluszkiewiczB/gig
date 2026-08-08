package gig_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/paluszkiewiczB/gig"
	"gopkg.in/yaml.v3"
)

func ExampleLoad() {
	type Config struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	}

	cfg, err := gig.Load[Config](strings.NewReader("host: localhost\nport: 8080\n"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %d\n", cfg.Host, cfg.Port)
	// Output: localhost 8080
}

func ExampleLoad_file() {
	type Config struct {
		Password string `yaml:"password"`
	}

	cfg, err := gig.Load[Config](
		strings.NewReader("password: !file testdata/password.txt\n"),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Password)
	// Output: s3cret
}

func ExampleWithSources() {
	type Config struct {
		Login string `yaml:"login"`
		Port  int    `yaml:"port"`
	}

	base := strings.NewReader("login: admin\nport: 8080\n")
	override := strings.NewReader("port: 9090\n")

	cfg, err := gig.Load[Config](base, gig.WithSources(override))
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %d\n", cfg.Login, cfg.Port)
	// Output: admin 9090
}

func ExampleWithEnvLookup() {
	type Config struct {
		Name string `yaml:"name"`
	}

	env := map[string]string{"SERVICE_NAME": "demo"}
	lookup := func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}

	cfg, err := gig.Load[Config](
		strings.NewReader("name: !env SERVICE_NAME\n"),
		gig.WithEnvLookup(lookup),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Name)
	// Output: demo
}

func Example_expressions() {
	type Config struct {
		Value string `yaml:"value"`
	}

	env := map[string]string{"USER": "alice", "MODE": "production"}
	lookup := func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}

	cfg, err := gig.Load[Config](
		strings.NewReader(`value: !env '${GREETING:-hello}'`),
		gig.WithEnvLookup(lookup),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("default:", cfg.Value)

	env["GREETING"] = "hi"
	cfg, err = gig.Load[Config](
		strings.NewReader(`value: !env '${GREETING:-hello}'`),
		gig.WithEnvLookup(lookup),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("value:", cfg.Value)

	cfg, err = gig.Load[Config](
		strings.NewReader(`value: !env '${MODE:+production-mode}'`),
		gig.WithEnvLookup(lookup),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("alternate:", cfg.Value)

	delete(env, "GREETING")
	cfg, err = gig.Load[Config](
		strings.NewReader(`value: !env '${GREETING:-${MODE:-fallback}}'`),
		gig.WithEnvLookup(lookup),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("nested:", cfg.Value)

	// Output:
	// default: hello
	// value: hi
	// alternate: production-mode
	// nested: production
}

func ExampleWithResolver() {
	type Config struct {
		Name string `yaml:"name"`
	}

	cfg, err := gig.Load[Config](
		strings.NewReader("name: !upper hello\n"),
		gig.WithResolver("!upper", func(_ context.Context, node *yaml.Node) error {
			node.Tag = ""
			node.Value = strings.ToUpper(node.Value)
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Name)
	// Output: HELLO
}
