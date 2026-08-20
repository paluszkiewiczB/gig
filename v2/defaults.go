package gig

import "os"

func buildDefaultMutators(fileOpts []FileOption, envOpts []EnvOption) ([]Mutator, error) {
	envCfg := &envConfig{
		lookup: osLookupEnv,
	}
	for _, opt := range envOpts {
		if err := opt(envCfg); err != nil {
			return nil, err
		}
	}

	fileCfg := &fileConfig{
		envLookup: envCfg.lookup,
	}
	for _, opt := range fileOpts {
		if err := opt(fileCfg); err != nil {
			return nil, err
		}
	}
	if fileCfg.fsys != nil && fileCfg.root != nil {
		fileCfg.fsys = nil
	}
	if fileCfg.root != nil && fileCfg.fsys == nil {
		fileCfg.fsys = fileCfg.root.FS()
	}

	return []Mutator{
		NewTagResolver(map[string]Mutator{
			"!env":   &envHandler{cfg: envCfg},
			"!env?":  &envHandler{cfg: envCfg},
			"!file":  &fileHandler{cfg: fileCfg},
			"!file?": &fileHandler{cfg: fileCfg},
		}),
	}, nil
}

var osLookupEnv = os.LookupEnv

// DefaultMutators returns the default mutator chain: a TagResolver handling
// !env, !env?, !file, and !file?.
func DefaultMutators() []Mutator {
	return []Mutator{
		NewTagResolver(map[string]Mutator{
			"!env":   &envHandler{cfg: &envConfig{lookup: osLookupEnv}},
			"!env?":  &envHandler{cfg: &envConfig{lookup: osLookupEnv}},
			"!file":  &fileHandler{cfg: &fileConfig{envLookup: osLookupEnv}},
			"!file?": &fileHandler{cfg: &fileConfig{envLookup: osLookupEnv}},
		}),
	}
}
