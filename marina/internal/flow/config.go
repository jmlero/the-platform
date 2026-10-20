// Package flow connects bounded repository work to durable ports.
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

	"github.com/jmlero/the-platform/marina/internal/conversation"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

type Config struct {
	Version                    int                         `json:"version,omitempty"`
	Conversation               *conversation.Profile       `json:"conversation,omitempty"`
	ProjectID                  string                      `json:"project_id"`
	PublicURL                  string                      `json:"public_url"`
	TLSCertFile                string                      `json:"tls_cert_file"`
	TLSKeyFile                 string                      `json:"tls_key_file"`
	SigningKeyFile             string                      `json:"signing_key_file"`
	ArtifactDirectory          string                      `json:"artifact_directory"`
	ArtifactTokenFile          string                      `json:"artifact_token_file"`
	Kubernetes                 kubernetes.Config           `json:"kubernetes"`
	RepositoryEngine           *runtimeapi.EngineSelection `json:"repository_engine,omitempty"`
	RepositoryEngineGatewayURL string                      `json:"repository_engine_gateway_url,omitempty"`
	Git                        github.Config               `json:"git"`
	Model                      workerapi.Model             `json:"model"`
	ReviewerModel              *workerapi.Model            `json:"reviewer_model,omitempty"`
	AllowedPaths               []string                    `json:"allowed_paths"`
	Checks                     []workresult.Check          `json:"checks"`
	RequiredCI                 []string                    `json:"required_ci"`
	Delivery                   string                      `json:"delivery,omitempty"`
	RequireApproval            bool                        `json:"require_approval"`
	// SkipReview and RepairFailures come only from a Run's Workload choices;
	// omitted, they keep the installation's review and three-failure limit.
	SkipReview     bool   `json:"skip_review,omitempty"`
	RepairFailures *int   `json:"repair_failures,omitempty"`
	MaxTokens      int64  `json:"max_tokens"`
	BudgetMinutes  int    `json:"budget_minutes"`
	PolicyVersion  string `json:"policy_version"`
}

func safeURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")))
}

func (c Config) Validate() error {
	if c.RepositoryEnabled() && c.RepositoryEngine == nil {
		return errors.New("repository work requires the native kagent engine; legacy agent Jobs are retired")
	}
	if c.Version < 0 || c.Version > 1 {
		return errors.New("unsupported workflow configuration version")
	}
	if c.Delivery != "" && c.Delivery != "pull_request" && c.Delivery != "fast_forward" {
		return errors.New("unsupported repository delivery mode")
	}
	if c.Delivery == "fast_forward" && (!c.RepositoryEnabled() || len(c.RequiredCI) == 0) {
		return errors.New("fast-forward delivery requires repository binding and required CI")
	}
	if c.Delivery == "fast_forward" {
		seenCI := make(map[string]bool, len(c.RequiredCI))
		for _, check := range c.RequiredCI {
			if strings.TrimSpace(check) == "" || seenCI[check] {
				return errors.New("fast-forward delivery requires distinct nonempty CI check names")
			}
			seenCI[check] = true
		}
	}
	if c.ProjectID == "" || c.PolicyVersion == "" || !safeURL(c.PublicURL) || !safeURL(c.Model.Endpoint) ||
		c.Model.Name == "" || c.Model.MaxOutputTokens < 1 || c.Model.MaxOutputTokens > 65536 || c.MaxTokens < 10000 || c.MaxTokens > 10000000 ||
		c.BudgetMinutes < 1 || c.BudgetMinutes > 1440 ||
		!filepath.IsAbs(c.ArtifactDirectory) || c.SigningKeyFile == "" || c.ArtifactTokenFile == "" {
		return errors.New("explicit project, HTTPS destinations, identity, secrets, bounded budget, file scope and trusted checks are required")
	}
	if c.Conversation != nil {
		if err := c.Conversation.Validate(c.Model); err != nil {
			return err
		}
		if c.Conversation.MaxTokens > c.MaxTokens || c.Conversation.BudgetMinutes > c.BudgetMinutes {
			return errors.New("conversation limits cannot exceed installation limits")
		}
		if c.Conversation.Delegation != nil {
			return errors.New("conversation.delegation is retired: a Turn proposes tasks and its client decides what to run (decision 0033)")
		}
	}
	if c.RepositoryEngine != nil {
		if err := c.RepositoryEngine.ValidateProfile(); err != nil {
			return err
		}
		if c.RepositoryEngineGatewayURL != "https://marina.marina-system.svc" ||
			c.RepositoryEngine.Version != kagent.Version || c.RepositoryEngine.Endpoint != "kint-engine-controller.kint-engine.svc:8083" ||
			c.RepositoryEngine.Namespace != "kint-engine" || c.RepositoryEngine.Harness != "kint-codex" || !c.RepositoryEnabled() {
			return errors.New("repository engine requires the pinned native Codex profile and repository binding")
		}
	}
	if c.RepositoryEngine == nil && c.RepositoryEngineGatewayURL != "" {
		return errors.New("repository engine gateway requires a selected engine")
	}
	if c.RepositoryEnabled() {
		if err := c.ValidateReviewer(); err != nil {
			return err
		}
		if !safeURL(c.Git.APIURL) || !safeURL(c.Git.CloneURL) || c.Git.Repository == "" || c.Git.AuthorName == "" || c.Git.AuthorEmail == "" || c.Git.BaseBranch == "" || len(c.Checks) == 0 || len(c.Checks) > 20 || len(c.AllowedPaths) == 0 {
			return errors.New("repository profile requires explicit Git destinations, identity, scope and trusted checks")
		}
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
		if scope != "." && !workresult.AllowedPath([]string{"."}, scope) {
			return errors.New("invalid allowed path")
		}
	}
	return nil
}

func (c Config) ValidateReviewer() error {
	m := c.ReviewerModel
	if m == nil || strings.TrimSpace(m.Name) == "" || strings.EqualFold(strings.TrimSpace(m.Name), strings.TrimSpace(c.Model.Name)) ||
		!safeURL(m.Endpoint) || m.MaxOutputTokens < 1 || m.MaxOutputTokens > 65536 || m.Currency != c.Model.Currency ||
		m.InputMicrosPerMillion < 0 || m.OutputMicrosPerMillion < 0 || m.InputMicrosPerMillion > 100000000 || m.OutputMicrosPerMillion > 100000000 {
		return errors.New("repository review requires a different explicitly configured model, HTTPS destination, bounded output and matching currency")
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

func (c Config) RepositoryEnabled() bool {
	return c.Git.Repository != "" || c.Git.CloneURL != "" || c.Git.APIURL != ""
}
