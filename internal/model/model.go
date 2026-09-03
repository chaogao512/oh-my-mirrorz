package model

import (
	"fmt"
	"io/fs"
	"time"
)

type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeSystem Scope = "system"
)

type DetectionStatus string

const (
	StatusDetected      DetectionStatus = "detected"
	StatusNotInstalled  DetectionStatus = "not-installed"
	StatusUnsupported   DetectionStatus = "unsupported"
	StatusInvalidConfig DetectionStatus = "invalid-config"
)

type Detection struct {
	AdapterID   string          `json:"adapter_id"`
	Status      DetectionStatus `json:"status"`
	Version     string          `json:"version,omitempty"`
	Scope       Scope           `json:"scope"`
	ConfigPaths []string        `json:"config_paths,omitempty"`
	Reason      string          `json:"reason,omitempty"`
}

type Strategy string

const (
	StrategyAuto   Strategy = "auto"
	StrategyFixed  Strategy = "fixed"
	StrategyPrefer Strategy = "prefer"
)

func ParseStrategy(value string) (Strategy, error) {
	s := Strategy(value)
	switch s {
	case StrategyAuto, StrategyFixed, StrategyPrefer:
		return s, nil
	default:
		return "", fmt.Errorf("unknown strategy %q", value)
	}
}

type Selection struct {
	AdapterID  string            `json:"adapter_id"`
	Provider   string            `json:"provider"`
	Mirror     string            `json:"mirror"`
	Endpoint   string            `json:"endpoint"`
	Endpoints  map[string]string `json:"endpoints,omitempty"`
	Fallbacks  []string          `json:"fallbacks,omitempty"`
	Strategy   Strategy          `json:"strategy"`
	Reason     string            `json:"reason"`
	ResolvedAt time.Time         `json:"resolved_at"`
}

type Change struct {
	AdapterID string      `json:"adapter_id"`
	Path      string      `json:"path"`
	Scope     Scope       `json:"scope"`
	Before    []byte      `json:"-"`
	After     []byte      `json:"-"`
	Mode      fs.FileMode `json:"mode"`
	Existed   bool        `json:"existed"`
	Remove    bool        `json:"remove,omitempty"`
}

func (c Change) Changed() bool {
	if c.Remove {
		return c.Existed
	}
	if !c.Existed {
		return len(c.After) > 0
	}
	if len(c.Before) != len(c.After) {
		return true
	}
	for i := range c.Before {
		if c.Before[i] != c.After[i] {
			return true
		}
	}
	return false
}

type Verification struct {
	AdapterID string `json:"adapter_id"`
	OK        bool   `json:"ok"`
	Detail    string `json:"detail"`
}

type Environment struct {
	Home               string
	XDGConfigHome      string
	XDGStateHome       string
	XDGCacheHome       string
	CargoHome          string
	HomebrewConfigHome string
	Shell              string
	GOOS               string
	GOARCH             string
	SystemRoot         string
	IncludeSystem      bool
	IncludeSecurity    bool
	PipConfigOverride  bool
	PipIndexOverride   bool
	UVIndexOverride    bool
	NPMConfigOverride  bool
}
