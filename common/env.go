package common

import (
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/constant"
)

func GetEnvOrDefault(env string, defaultValue int) int {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	num, err := strconv.Atoi(os.Getenv(env))
	if err != nil {
		SysError(LogText("failed to parse %s: %s, using default value: %d", env, err.Error(), defaultValue))
		return defaultValue
	}
	return num
}

// RelayCancelUsageTimeoutFromEnv reads the validated wait in seconds without
// changing the running server's configuration.
func RelayCancelUsageTimeoutFromEnv() int {
	const name = "RELAY_CANCEL_USAGE_TIMEOUT"
	seconds := GetEnvOrDefault(name, constant.DefaultRelayCancelUsageTimeout)
	if seconds < 0 || seconds > constant.MaxRelayCancelUsageTimeout {
		SysError(LogText("%s must be between 0 and %d, using default value: %d", name, constant.MaxRelayCancelUsageTimeout, constant.DefaultRelayCancelUsageTimeout))
		return constant.DefaultRelayCancelUsageTimeout
	}
	return seconds
}

func GetEnvOrDefaultString(env string, defaultValue string) string {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	return os.Getenv(env)
}

func GetEnvOrDefaultBool(env string, defaultValue bool) bool {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(os.Getenv(env))
	if err != nil {
		SysError(LogText("failed to parse %s: %s, using default value: %t", env, err.Error(), defaultValue))
		return defaultValue
	}
	return b
}
