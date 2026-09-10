package executor

import (
	"strconv"
	"strings"
)

// Limits defines hard resource constraints for execution containers.
type Limits struct {
	Memory      int64  // bytes
	CPUPeriod   int64
	CPUQuota    int64
	PidsLimit   int
	TimeoutSecs int
}

// DefaultLimits returns the standard resource limits for code execution.
func DefaultLimits() *Limits {
	return &Limits{
		Memory:      parseMemory("256m"),
		CPUPeriod:   100000,
		CPUQuota:    50000, // 0.5 CPU
		PidsLimit:   64,
		TimeoutSecs: 30,
	}
}

// WithMemory sets the memory limit from a Docker-format string (e.g. "256m", "1g").
func (l *Limits) WithMemory(s string) *Limits {
	l.Memory = parseMemory(s)
	return l
}

// WithCPUQuota sets the CPU quota (per 100000 period).
func (l *Limits) WithCPUQuota(quota int64) *Limits {
	l.CPUQuota = quota
	return l
}

// WithPidsLimit sets the max number of processes.
func (l *Limits) WithPidsLimit(n int) *Limits {
	l.PidsLimit = n
	return l
}

// WithTimeout sets the max execution time in seconds.
func (l *Limits) WithTimeout(secs int) *Limits {
	l.TimeoutSecs = secs
	return l
}



// parseMemory converts a human-readable memory string to bytes.
// Supports: "128m", "256m", "512m", "1g", "2g"
func parseMemory(s string) int64 {
	if s == "" {
		return 256 * 1024 * 1024 // default 256MB
	}

	s = strings.TrimSpace(strings.ToLower(s))
	multiplier := int64(1)

	if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = s[:len(s)-1]
	}

	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v * multiplier
	}
	return 256 * 1024 * 1024 // fallback 256MB
}
