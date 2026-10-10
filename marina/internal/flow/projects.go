package flow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// LoadProjects accepts the original single profile or an explicit installation
// containing complete per-project profiles. Repository and credential choices
// stay private and Marina-owned; a Deck repository request grants neither.
func LoadProjects(path string) ([]Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("project configuration exceeds size limit")
	}
	var header map[string]json.RawMessage
	if json.Unmarshal(raw, &header) != nil {
		return nil, errors.New("invalid project configuration")
	}
	if _, ok := header["projects"]; !ok {
		config, err := Load(path)
		if err != nil {
			return nil, err
		}
		return []Config{config}, nil
	}
	var installation struct {
		Version  int      `json:"version"`
		Projects []Config `json:"projects"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&installation) != nil || decoder.Decode(new(any)) != io.EOF || installation.Version != 1 || len(installation.Projects) < 1 || len(installation.Projects) > 16 {
		return nil, errors.New("bounded version-1 project profiles required")
	}
	shared := installation.Projects[0]
	seen := map[string]bool{}
	for _, config := range installation.Projects {
		if err := config.Validate(); err != nil {
			return nil, err
		}
		if seen[config.ProjectID] {
			return nil, errors.New("each project needs one profile")
		}
		seen[config.ProjectID] = true
		if config.PublicURL != shared.PublicURL || config.TLSCertFile != shared.TLSCertFile || config.TLSKeyFile != shared.TLSKeyFile || config.SigningKeyFile != shared.SigningKeyFile || config.ArtifactDirectory != shared.ArtifactDirectory || config.ArtifactTokenFile != shared.ArtifactTokenFile {
			return nil, errors.New("project profiles must share the service TLS, signing and artifact installation")
		}
	}
	return installation.Projects, nil
}
