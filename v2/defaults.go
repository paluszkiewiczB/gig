package gig

import "os"

func buildDefaultMutators(fileOpts []FileOption, envOpts []EnvOption) ([]Mutator, error) {
	envCfg := &envConfig{
		lookup:   os.LookupEnv,
		expander: nil,
	}
	for _, opt := range envOpts {
		if err := opt(envCfg); err != nil {
			return nil, err
		}
	}

	fileCfg := &fileConfig{
		baseDir:   "",
		fsys:      nil,
		root:      nil,
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

	return []Mutator{newDefaultTagResolver(envCfg, fileCfg)}, nil
}

// DefaultMutators returns the default mutator chain: a TagResolver handling
// !env, !env?, !file, and !file?.
func DefaultMutators() []Mutator {
	envCfg := &envConfig{
		lookup:   os.LookupEnv,
		expander: nil,
	}
	fileCfg := &fileConfig{
		baseDir:   "",
		fsys:      nil,
		root:      nil,
		envLookup: os.LookupEnv,
	}

	return []Mutator{newDefaultTagResolver(envCfg, fileCfg)}
}

func newDefaultTagResolver(envCfg *envConfig, fileCfg *fileConfig) *TagResolver {
	return NewTagResolver(map[string]Mutator{
		"!env":   &envHandler{cfg: envCfg},
		"!env?":  &envHandler{cfg: envCfg},
		"!file":  &fileHandler{cfg: fileCfg},
		"!file?": &fileHandler{cfg: fileCfg},
	})
}
