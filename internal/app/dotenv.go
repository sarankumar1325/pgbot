package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

var localEnvAllowlist = map[string]bool{
	"QUERYSHIFT_AI_PROVIDER": true, "QUERYSHIFT_AI_MODEL": true,
	"QUERYSHIFT_AI_BASE_URL": true, "QUERYSHIFT_AI_API_KEY": true,
	"QUERYSHIFT_AI_REASONING_EFFORT": true, "QUERYSHIFT_AGENT_MODEL": true,
	"AWS_REGION": true, "AWS_DEFAULT_REGION": true,
	"AWS_BEARER_TOKEN_BEDROCK": true, "AWS_ACCESS_KEY_ID": true,
	"AWS_SECRET_ACCESS_KEY": true, "AWS_SESSION_TOKEN": true,
	"AWS_CREDENTIAL_EXPIRATION": true,
}

// LoadLocalEnv reads the small model-configuration allowlist from a local .env
// without replacing values already exported by the user's shell. Connection
// strings are intentionally excluded: the TUI keeps those in memory only.
func LoadLocalEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return fmt.Errorf("%s:%d: expected KEY=value", path, lineNumber)
		}
		if !localEnvAllowlist[key] || os.Getenv(key) != "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
				value = value[1 : len(value)-1]
			}
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}
