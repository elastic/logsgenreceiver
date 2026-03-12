package logsgenreceiver

import (
	"embed"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/profiles.yaml
var builtinProfilesFS embed.FS

const builtinProfilesPath = "builtin/profiles.yaml"

var (
	builtinProfiles     map[string]*ProfileCfg
	builtinProfilesOnce sync.Once
	builtinProfilesErr  error
)

type builtinProfilesFile struct {
	Profiles []ProfileCfg `yaml:"profiles"`
}

func loadBuiltinProfiles() (map[string]*ProfileCfg, error) {
	builtinProfilesOnce.Do(func() {
		data, err := builtinProfilesFS.ReadFile(builtinProfilesPath)
		if err != nil {
			builtinProfilesErr = fmt.Errorf("read built-in profiles: %w", err)
			return
		}
		var file builtinProfilesFile
		if err := yaml.Unmarshal(data, &file); err != nil {
			builtinProfilesErr = fmt.Errorf("parse built-in profiles: %w", err)
			return
		}
		builtinProfiles = make(map[string]*ProfileCfg, len(file.Profiles))
		for i := range file.Profiles {
			p := &file.Profiles[i]
			if p.Name == "" {
				builtinProfilesErr = fmt.Errorf("built-in profile at index %d has empty name", i)
				return
			}
			if _, exists := builtinProfiles[p.Name]; exists {
				builtinProfilesErr = fmt.Errorf("duplicate built-in profile name %q", p.Name)
				return
			}
			if len(p.Scenarios) == 0 {
				builtinProfilesErr = fmt.Errorf("built-in profile %q has no scenarios", p.Name)
				return
			}
			if err := validateScenarios(p.Scenarios); err != nil {
				builtinProfilesErr = fmt.Errorf("built-in profile %q: %w", p.Name, err)
				return
			}
			builtinProfiles[p.Name] = p
		}
	})
	return builtinProfiles, builtinProfilesErr
}

// getBuiltinProfile returns the built-in profile with the given name, or (nil, false) if not found.
// Built-in profiles are loaded from the embedded profiles.yaml on first call.
func getBuiltinProfile(name string) (*ProfileCfg, bool) {
	profiles, err := loadBuiltinProfiles()
	if err != nil || profiles == nil {
		return nil, false
	}
	p, ok := profiles[name]
	return p, ok
}
