// Package flow connects the supported repository-fix workflow to durable ports.
package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

type Config struct {
	ProjectID         string            `json:"project_id"`
	PublicURL         string            `json:"public_url"`
	TLSCertFile       string            `json:"tls_cert_file"`
	TLSKeyFile        string            `json:"tls_key_file"`
	SigningKeyFile    string            `json:"signing_key_file"`
	ArtifactDirectory string            `json:"artifact_directory"`
	ArtifactTokenFile string            `json:"artifact_token_file"`
	Kubernetes        kubernetes.Config `json:"kubernetes"`
	Git               github.Config     `json:"git"`
	Model             workerapi.Model   `json:"model"`
	AllowedPaths      []string          `json:"allowed_paths"`
	Checks            []worker.Check    `json:"checks"`
	RequiredCI        []string          `json:"required_ci"`
	RequireApproval   bool              `json:"require_approval"`
	MaxTokens         int64             `json:"max_tokens"`
	BudgetMinutes     int               `json:"budget_minutes"`
	PolicyVersion     string            `json:"policy_version"`
}

func safeURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")))
}

func (c Config) Validate() error {
	if c.ProjectID == "" || c.PolicyVersion == "" || !safeURL(c.PublicURL) || !safeURL(c.Git.APIURL) || !safeURL(c.Git.CloneURL) || !safeURL(c.Model.Endpoint) ||
		c.Model.Name == "" || c.Model.MaxOutputTokens < 1 || c.Model.MaxOutputTokens > 65536 || c.MaxTokens < 10000 || c.MaxTokens > 10000000 ||
		c.BudgetMinutes < 1 || c.BudgetMinutes > 1440 || len(c.Checks) == 0 || len(c.Checks) > 20 || len(c.AllowedPaths) == 0 ||
		!filepath.IsAbs(c.ArtifactDirectory) || c.SigningKeyFile == "" || c.ArtifactTokenFile == "" || c.Git.AuthorName == "" || c.Git.AuthorEmail == "" || c.Git.BaseBranch == "" {
		return errors.New("explicit project, HTTPS destinations, identity, secrets, bounded budget, file scope and trusted checks are required")
	}
	if strings.HasPrefix(c.PublicURL, "https:") && (c.TLSCertFile == "" || c.TLSKeyFile == "") {
		return errors.New("HTTPS worker gateway requires TLS certificate and key")
	}
	if c.Model.Currency == "" || c.Model.InputMicrosPerMillion < 0 || c.Model.OutputMicrosPerMillion < 0 || c.Model.InputMicrosPerMillion > 100000000 || c.Model.OutputMicrosPerMillion > 100000000 {
		return errors.New("model currency and bounded price rates are required")
	}
	seen := map[string]bool{}
	for _, check := range c.Checks {
		if check.ID == "" || seen[check.ID] || len(check.Command) == 0 || check.Command[0] == "" || check.Directory == "" || filepath.IsAbs(check.Directory) || strings.Contains(check.Directory, "..") {
			return errors.New("checks require unique IDs, commands and repository-relative directories")
		}
		seen[check.ID] = true
	}
	for _, scope := range c.AllowedPaths {
		if scope != "." && !worker.AllowedPath([]string{"."}, scope) {
			return errors.New("invalid allowed path")
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	var config Config
	f, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&config); err != nil {
		return config, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return config, errors.New("one configuration document required")
	}
	return config, config.Validate()
}

func (c Config) Digest() string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
