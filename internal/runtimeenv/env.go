package runtimeenv

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func Detect(includeSystem, includeSecurity bool) (model.Environment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return model.Environment{}, err
	}
	return FromHome(home, includeSystem, includeSecurity), nil
}

func FromHome(home string, includeSystem, includeSecurity bool) model.Environment {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	cargo := os.Getenv("CARGO_HOME")
	if cargo == "" {
		cargo = filepath.Join(home, ".cargo")
	}
	homebrewConfig := ""
	if explicitXDG := os.Getenv("XDG_CONFIG_HOME"); explicitXDG != "" {
		homebrewConfig = filepath.Join(explicitXDG, "homebrew")
	} else if homebrewXDG := os.Getenv("HOMEBREW_XDG_CONFIG_HOME"); homebrewXDG != "" {
		homebrewConfig = filepath.Join(homebrewXDG, "homebrew")
	} else {
		homebrewConfig = filepath.Join(home, ".homebrew")
	}
	return model.Environment{
		Home:               home,
		XDGConfigHome:      config,
		XDGStateHome:       state,
		XDGCacheHome:       cache,
		CargoHome:          cargo,
		HomebrewConfigHome: homebrewConfig,
		Shell:              os.Getenv("SHELL"),
		GOOS:               runtime.GOOS,
		GOARCH:             runtime.GOARCH,
		SystemRoot:         "/",
		IncludeSystem:      includeSystem,
		IncludeSecurity:    includeSecurity,
		PipConfigOverride:  os.Getenv("PIP_CONFIG_FILE") != "",
		PipIndexOverride:   os.Getenv("PIP_INDEX_URL") != "",
		UVIndexOverride:    os.Getenv("UV_DEFAULT_INDEX") != "" || os.Getenv("UV_INDEX_URL") != "",
		NPMConfigOverride:  os.Getenv("NPM_CONFIG_USERCONFIG") != "" || os.Getenv("NPM_CONFIG_REGISTRY") != "",
	}
}
