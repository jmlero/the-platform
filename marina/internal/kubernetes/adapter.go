// Package kubernetes runs disposable workers using the Kubernetes batch/v1 API.
// It deliberately owns no task, plan, or evidence policy.
package kubernetes

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

var dnsName = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

type Config struct {
	ServerURL         string                                                       `json:"server_url"`
	Namespace         string                                                       `json:"namespace"`
	TokenFile         string                                                       `json:"token_file"`
	CAFile            string                                                       `json:"ca_file"`
	Image             string                                                       `json:"image"`
	WorkerURL         string                                                       `json:"worker_url"`
	ServiceAccount    string                                                       `json:"service_account"`
	WorkerCAConfigMap string                                                       `json:"worker_ca_config_map"`
	RuntimeClass      string                                                       `json:"runtime_class,omitempty"`
	SigningKey        []byte                                                       `json:"-"`
	Artifacts         func(context.Context, string) ([]runtimeapi.Artifact, error) `json:"-"`
}

type Adapter struct {
	config Config
	client *http.Client
}

var _ runtimeapi.Adapter = (*Adapter)(nil)

func New(config Config, client *http.Client) (*Adapter, error) {
	if !dnsName.MatchString(config.Namespace) || !dnsName.MatchString(config.ServiceAccount) || len(config.SigningKey) < 32 ||
		!regexp.MustCompile(`^.+@sha256:[a-f0-9]{64}$`).MatchString(config.Image) {
		return nil, errors.New("worker namespace, service account, image digest, and signing key are required")
	}
	for _, raw := range []string{config.ServerURL, config.WorkerURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
			return nil, errors.New("Kubernetes and worker endpoints require HTTPS (loopback HTTP is allowed for tests)")
		}
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if config.CAFile != "" {
			pem, err := os.ReadFile(config.CAFile)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("invalid Kubernetes CA")
			}
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
		}
		client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Adapter{config: config, client: &copyClient}, nil
}

func Token(key []byte, specification runtimeapi.Specification) string {
	digest, _ := specification.Digest()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(specification.AttemptID + "\x00" + digest))
	return hex.EncodeToString(mac.Sum(nil))
}

func jobName(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "marina-" + hex.EncodeToString(digest[:20])
}
func (adapter *Adapter) jobs() string {
	return "/apis/batch/v1/namespaces/" + adapter.config.Namespace + "/jobs"
}
func (adapter *Adapter) secrets() string {
	return "/api/v1/namespaces/" + adapter.config.Namespace + "/secrets"
}

type job struct {
	Metadata struct {
		Name        string            `json:"name"`
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		Active     int `json:"active"`
		Succeeded  int `json:"succeeded"`
		Failed     int `json:"failed"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"conditions"`
	} `json:"status"`
}

func (adapter *Adapter) Start(ctx context.Context, spec runtimeapi.Specification) (runtimeapi.Observation, error) {
	if err := spec.Validate(time.Now().UTC()); err != nil {
		return runtimeapi.Observation{}, err
	}
	name := jobName(spec.IdempotencyKey)
	digest, _ := spec.Digest()
	var existing job
	status, err := adapter.request(ctx, http.MethodGet, adapter.jobs()+"/"+name, nil, &existing)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if status == http.StatusOK {
		if existing.Metadata.Annotations["marina.dev/specification"] != digest {
			return runtimeapi.Observation{}, runtimeapi.ErrConflict
		}
		return adapter.observation(existing), nil
	}
	if status != http.StatusNotFound {
		return runtimeapi.Observation{}, apiError("inspect Job", status)
	}
	secret := map[string]any{"apiVersion": "v1", "kind": "Secret", "immutable": true,
		"metadata":   map[string]any{"name": name, "labels": map[string]string{"app.kubernetes.io/name": "marina-worker"}},
		"stringData": map[string]string{"token": Token(adapter.config.SigningKey, spec)}}
	status, err = adapter.request(ctx, http.MethodPost, adapter.secrets(), secret, nil)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if status != http.StatusCreated && status != http.StatusConflict {
		return runtimeapi.Observation{}, apiError("create worker credential", status)
	}
	var created job
	status, err = adapter.request(ctx, http.MethodPost, adapter.jobs(), adapter.manifest(spec, name, digest), &created)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if status == http.StatusConflict {
		status, err = adapter.request(ctx, http.MethodGet, adapter.jobs()+"/"+name, nil, &existing)
		if err != nil {
			return runtimeapi.Observation{}, err
		}
		if status != http.StatusOK {
			return runtimeapi.Observation{}, apiError("recover conflicting Job", status)
		}
		if existing.Metadata.Annotations["marina.dev/specification"] != digest {
			return runtimeapi.Observation{}, runtimeapi.ErrConflict
		}
		return adapter.observation(existing), nil
	}
	if status != http.StatusCreated {
		return runtimeapi.Observation{}, apiError("start Job", status)
	}
	return adapter.observation(created), nil
}

func (adapter *Adapter) manifest(spec runtimeapi.Specification, name, digest string) map[string]any {
	labels := map[string]string{"app.kubernetes.io/name": "marina-worker", "marina.dev/attempt": spec.AttemptID}
	volumes := []any{map[string]any{"name": "work", "emptyDir": map[string]any{"sizeLimit": strconv.FormatInt(spec.Limits.EphemeralStorageBytes, 10)}},
		map[string]any{"name": "credential", "secret": map[string]any{"secretName": name, "defaultMode": int32(0440)}}}
	mounts := []any{map[string]any{"name": "work", "mountPath": "/work"}, map[string]any{"name": "credential", "mountPath": "/run/marina", "readOnly": true}}
	env := []any{map[string]string{"name": "MARINA_WORKER_URL", "value": adapter.config.WorkerURL},
		map[string]string{"name": "MARINA_ATTEMPT_ID", "value": spec.AttemptID}, map[string]string{"name": "MARINA_WORKER_TOKEN_FILE", "value": "/run/marina/token"}}
	if adapter.config.WorkerCAConfigMap != "" {
		volumes = append(volumes, map[string]any{"name": "ca", "configMap": map[string]any{"name": adapter.config.WorkerCAConfigMap}})
		mounts = append(mounts, map[string]any{"name": "ca", "mountPath": "/run/marina-ca", "readOnly": true})
		env = append(env, map[string]string{"name": "MARINA_WORKER_CA_FILE", "value": "/run/marina-ca/ca.crt"})
	}
	seconds := min(spec.Limits.WallTimeSeconds, int64(time.Until(spec.DeadlineAt).Seconds()))
	pod := map[string]any{"serviceAccountName": adapter.config.ServiceAccount, "automountServiceAccountToken": false,
		"enableServiceLinks": false, "restartPolicy": "Never", "terminationGracePeriodSeconds": 10,
		"securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532, "fsGroup": 65532, "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
		"containers": []any{map[string]any{"name": "worker", "image": adapter.config.Image, "imagePullPolicy": "IfNotPresent",
			"command": []string{"/marina", "worker"}, "env": env, "volumeMounts": mounts,
			"securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}},
			"resources": map[string]any{"requests": map[string]string{"cpu": "100m", "memory": "128Mi"},
				"limits": map[string]string{"cpu": fmt.Sprintf("%dm", spec.Limits.CPUMillis), "memory": strconv.FormatInt(spec.Limits.MemoryBytes, 10), "ephemeral-storage": strconv.FormatInt(spec.Limits.EphemeralStorageBytes, 10)}}}}, "volumes": volumes}
	if adapter.config.RuntimeClass != "" {
		pod["runtimeClassName"] = adapter.config.RuntimeClass
	}
	return map[string]any{"apiVersion": "batch/v1", "kind": "Job", "metadata": map[string]any{"name": name, "labels": labels,
		"annotations": map[string]string{"marina.dev/specification": digest, "marina.dev/attempt": spec.AttemptID}},
		"spec": map[string]any{"backoffLimit": 0, "activeDeadlineSeconds": max(int64(1), seconds),
			"template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": pod}}}
}

func (adapter *Adapter) observation(value job) runtimeapi.Observation {
	state := runtimeapi.StateStarting
	if value.Status.Active > 0 {
		state = runtimeapi.StateRunning
	}
	for _, condition := range value.Status.Conditions {
		if condition.Status != "True" {
			continue
		}
		if condition.Type == "Complete" {
			state = runtimeapi.StateSucceeded
		}
		if condition.Type == "Failed" {
			state = runtimeapi.StateFailed
			if condition.Reason == "DeadlineExceeded" {
				state = runtimeapi.StateTimedOut
			}
		}
	}
	return runtimeapi.Observation{Found: true, ProviderHandle: value.Metadata.Name + "/" + value.Metadata.UID, State: state}
}

func splitHandle(handle string) (string, string, error) {
	parts := strings.Split(handle, "/")
	if len(parts) != 2 || !dnsName.MatchString(parts[0]) || parts[1] == "" {
		return "", "", runtimeapi.ErrInvalidInput
	}
	return parts[0], parts[1], nil
}

func (adapter *Adapter) Inspect(ctx context.Context, handle string) (runtimeapi.Observation, error) {
	name, uid, err := splitHandle(handle)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	var value job
	status, err := adapter.request(ctx, http.MethodGet, adapter.jobs()+"/"+name, nil, &value)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if status == 404 {
		return runtimeapi.Observation{}, nil
	}
	if status != 200 {
		return runtimeapi.Observation{}, apiError("inspect Job", status)
	}
	if value.Metadata.UID != uid {
		return runtimeapi.Observation{}, runtimeapi.ErrConflict
	}
	return adapter.observation(value), nil
}

func (adapter *Adapter) InspectByKey(ctx context.Context, key string) (runtimeapi.Observation, error) {
	var value job
	status, err := adapter.request(ctx, http.MethodGet, adapter.jobs()+"/"+jobName(key), nil, &value)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if status == 404 {
		return runtimeapi.Observation{}, nil
	}
	if status != 200 {
		return runtimeapi.Observation{}, apiError("recover Job", status)
	}
	return adapter.observation(value), nil
}

func (adapter *Adapter) Resume(context.Context, string) (runtimeapi.Observation, error) {
	return runtimeapi.Observation{}, runtimeapi.ErrUnsupportedResume
}
func (adapter *Adapter) Capabilities() runtimeapi.Capabilities { return runtimeapi.Capabilities{} }

func (adapter *Adapter) Cancel(ctx context.Context, handle string, force bool) (runtimeapi.Observation, error) {
	name, uid, err := splitHandle(handle)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	grace := 10
	if force {
		grace = 0
	}
	status, err := adapter.request(ctx, http.MethodDelete, adapter.jobs()+"/"+name, map[string]any{
		"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Foreground", "gracePeriodSeconds": grace, "preconditions": map[string]string{"uid": uid}}, nil)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if status != 200 && status != 202 && status != 404 {
		return runtimeapi.Observation{}, apiError("cancel Job", status)
	}
	observed, err := adapter.Inspect(ctx, handle)
	if err != nil {
		return runtimeapi.Observation{}, err
	}
	if !observed.Found {
		return runtimeapi.Observation{Found: true, ProviderHandle: handle, State: runtimeapi.StateCancelled}, nil
	}
	return observed, nil
}

func (adapter *Adapter) CollectArtifacts(ctx context.Context, handle string) ([]runtimeapi.Artifact, error) {
	if adapter.config.Artifacts == nil {
		return nil, errors.New("durable artifact collector is not configured")
	}
	return adapter.config.Artifacts(ctx, handle)
}

func (adapter *Adapter) Cleanup(ctx context.Context, handle string) error {
	name, _, err := splitHandle(handle)
	if err != nil {
		return err
	}
	observed, err := adapter.Cancel(ctx, handle, true)
	if err != nil {
		return err
	}
	if !observed.State.Terminal() {
		return errors.New("Job deletion is still pending")
	}
	status, err := adapter.request(ctx, http.MethodDelete, adapter.secrets()+"/"+name, nil, nil)
	if err != nil {
		return err
	}
	if status != 200 && status != 202 && status != 404 {
		return apiError("clean worker credential", status)
	}
	return nil
}

func apiError(action string, status int) error {
	return fmt.Errorf("Kubernetes %s returned HTTP %d", action, status)
}
func (adapter *Adapter) request(ctx context.Context, method, path string, body, result any) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(adapter.config.ServerURL, "/")+path, reader)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	if adapter.config.TokenFile != "" {
		token, err := os.ReadFile(adapter.config.TokenFile)
		if err != nil {
			return 0, errors.New("read Kubernetes credential")
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if result != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(result); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}
