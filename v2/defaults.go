package gig

import "os"

func buildDefaultMutators(fileOpts []FileOption, envOpts []EnvOption) []Mutator { //nolint:cyclop,unparam
	envCfg := &envConfig{
		lookup: osLookupEnv,
	}
	for _, opt := range envOpts {
		opt(envCfg)
	}

	fileCfg := &fileConfig{
		envLookup: envCfg.lookup,
	}
	for _, opt := range fileOpts {
		opt(fileCfg)
	}
	if fileCfg.envLookup == nil {
		fileCfg.envLookup = envCfg.lookup
	}

	if fileCfg.fsys != nil && fileCfg.root != nil {
		fileCfg.fsys = nil
	}
	if fileCfg.root != nil && fileCfg.fsys == nil {
		fileCfg.fsys = fileCfg.root.FS()
	}

	if envCfg.hasLookup && envCfg.lookup == nil {
		return []Mutator{errorMutator("env lookup must not be nil")}
	}
	if envCfg.hasExpander && envCfg.expander == nil {
		return []Mutator{errorMutator("env expander must not be nil")}
	}
	if fileCfg.hasRoot && fileCfg.root == nil {
		return []Mutator{errorMutator("root must not be nil")}
	}
	if fileCfg.hasFS && fileCfg.fsys == nil {
		return []Mutator{errorMutator("filesystem must not be nil")}
	}

	envHandler := &envHandler{cfg: envCfg}
	fileHandler := &fileHandler{cfg: fileCfg}

	return []Mutator{
		NewTagResolver(map[string]Mutator{
			"!env":   envHandler,
			"!env?":  envHandler,
			"!file":  fileHandler,
			"!file?": fileHandler,
		}),
	}
}

var osLookupEnv = os.LookupEnv

func DefaultMutators() []Mutator {
	return buildDefaultMutators(nil, nil)
}
