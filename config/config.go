package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the runner service.
type Config struct {
	Port           int
	DockerSocket   string
	ImagePrefix    string
	ExecTimeout    time.Duration
	MaxMemory      string // Docker format, e.g. "256m"
	CPUPeriod      int64
	CPUQuota       int64
	MaxPID         int
	AllowedOrigins []string
	PoolSize       int
	MaxConcurrent  int 
	RateLimitRPS   int
	RateLimitDaily int
	AuthSecret string
}



func loadEnvFile() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return // no .env file is fine
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[:1] == "\"" && val[len(val)-1:] == "\"") || (val[:1] == "'" && val[len(val)-1:] == "'") {
				val = val[1 : len(val)-1]
			}
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func Load() *Config {
	loadEnvFile()
	cfg := &Config{
		Port:           getEnvInt("PORT", 8080),
		DockerSocket:   getEnvStr("DOCKER_SOCKET", "/var/run/docker.sock"),
		ImagePrefix:    getEnvStr("IMAGE_PREFIX", "modigo-runner-"),
		ExecTimeout:    getEnvDuration("EXEC_TIMEOUT", 30*time.Second),
		MaxMemory:      getEnvStr("MAX_MEMORY", "256m"),
		CPUPeriod:      100000,
		CPUQuota:       getEnvInt64("CPU_QUOTA", 50000),
		MaxPID:         getEnvInt("MAX_PID", 64),
		AllowedOrigins: getEnvCSV("ALLOWED_ORIGINS", []string{"*"}),
		PoolSize:       getEnvInt("POOL_SIZE", 3),
		MaxConcurrent:  getEnvInt("MAX_CONCURRENT", 50),
		RateLimitRPS:   getEnvInt("RATE_LIMIT_RPS", 10),
		RateLimitDaily: getEnvInt("RATE_LIMIT_DAILY", 10000),
		AuthSecret:     getEnvStr("AUTH_SECRET", "e229f6ca55830a6741245bb382203c62ab5d9dfb66847bc004b914ed3b97679b"),
	}
	return cfg
}

// HasAuth returns true if JWT validation is configured.
func (c *Config) HasAuth() bool {
	return c.AuthSecret != ""
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getEnvCSV(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return fallback
}
