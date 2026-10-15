package gateway

import (
	"encoding/json"
	"os"
)

// AccountConfig contains per-account credentials in an ignored configuration file.
type AccountConfig struct {
	ID            string `json:"id"`
	APIID         int    `json:"apiId"`
	APIHash       string `json:"apiHash"`
	SessionPath   string `json:"sessionPath"`
	SessionFormat string `json:"sessionFormat"`
	Passcode      string `json:"passcode,omitempty"`
	DeviceModel   string `json:"deviceModel,omitempty"`
	SystemVersion string `json:"systemVersion,omitempty"`
	AppVersion    string `json:"appVersion,omitempty"`
}

// LoadAccountConfigs reads an explicitly selected ignored private file.
func LoadAccountConfigs(path string) ([]AccountConfig, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var c []AccountConfig
	e = json.Unmarshal(b, &c)
	return c, e
}
