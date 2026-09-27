package connections

import (
	"encoding/json"
	"os"
	"strings"
)

// Claude reports the persisted user gateway, independently of account login or
// activity. Shell/project overrides and temporary garcon claude launches do not
// change this saved default connection.
func Claude(path, base string) Status {
	status := Status{Client: "claude"}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		value := false
		status.Connected = &value
		return status
	}
	if err != nil {
		return status
	}
	var config struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal(b, &config) != nil {
		return status
	}
	value := sameEndpoint(config.Env["ANTHROPIC_BASE_URL"], strings.TrimRight(base, "/")+"/claude")
	status.Connected = &value
	return status
}
