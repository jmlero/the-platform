package kubernetes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
)

const (
	engineOwnerLabel = "the-platform.dev/owner"
	engineOwner      = "marina"
	engineAPI        = "/apis/kagent.dev/v1alpha3/namespaces/"
)

var _ runtimeapi.EngineTemplates = (*Adapter)(nil)

// EnsureEngineTemplate creates one attempt's gateway Secret placeholder,
// ModelConfig and AgentTemplate, or verifies those an earlier cycle created.
// It returns the prepared revision once the engine reports it Ready. A resource
// that Marina did not create, or whose content changed, is never adopted.
func (adapter *Adapter) EnsureEngineTemplate(ctx context.Context, t runtimeapi.EngineTemplate) (string, error) {
	if err := validEngineTemplateName(t.Namespace, t.Name); err != nil {
		return "", err
	}
	if !dnsName.MatchString(t.Harness) || t.Description == "" || t.SystemPrompt == "" || t.Model == "" || !strings.HasPrefix(t.BaseURL, "https://") {
		return "", runtimeapi.ErrInvalidInput
	}
	placeholder := make([]byte, 32)
	if _, err := rand.Read(placeholder); err != nil {
		return "", err
	}
	labels := map[string]any{engineOwnerLabel: engineOwner}
	secret := map[string]any{"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
		"metadata": map[string]any{"name": t.Name, "labels": labels},
		"data":     map[string]any{"gateway": base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(placeholder)))}}
	// Only the Secret's shape is compared: its value becomes the attempt's
	// credential after the specification that binds the revision is saved.
	if _, err := adapter.ensureEngineResource(ctx, "/api/v1/namespaces/"+t.Namespace+"/secrets", secret, func(existing map[string]any) bool {
		data, _ := existing["data"].(map[string]any)
		value, _ := data["gateway"].(string)
		return existing["type"] == "Opaque" && len(data) == 1 && value != ""
	}); err != nil {
		return "", err
	}
	model := map[string]any{"apiVersion": "kagent.dev/v1alpha3", "kind": "ModelConfig",
		"metadata": map[string]any{"name": t.Name + "-model", "labels": labels},
		"spec": map[string]any{"provider": "OpenAI", "model": t.Model, "apiKeySecret": t.Name, "apiKeySecretKey": "gateway",
			"openAI": map[string]any{"baseUrl": t.BaseURL, "apiFormat": "responses"}}}
	if _, err := adapter.ensureEngineResource(ctx, engineAPI+t.Namespace+"/modelconfigs", model, sameSpec(model)); err != nil {
		return "", err
	}
	template := map[string]any{"apiVersion": "kagent.dev/v1alpha3", "kind": "AgentTemplate",
		"metadata": map[string]any{"name": t.Name, "labels": map[string]any{engineOwnerLabel: engineOwner, "kagent.dev/harness": t.Harness}},
		"spec":     map[string]any{"modelConfig": map[string]any{"name": t.Name + "-model"}, "description": t.Description, "systemPrompt": t.SystemPrompt}}
	existing, err := adapter.ensureEngineResource(ctx, engineAPI+t.Namespace+"/agenttemplates", template, func(existing map[string]any) bool {
		metadata, _ := existing["metadata"].(map[string]any)
		labels, _ := metadata["labels"].(map[string]any)
		return labels["kagent.dev/harness"] == t.Harness && sameSpec(template)(existing)
	})
	if err != nil {
		return "", err
	}
	return preparedRevision(existing, t.Harness)
}

// DeleteEngineTemplate removes one attempt's AgentTemplate, ModelConfig and
// Secret after its engine instance is gone. Missing resources are success.
func (adapter *Adapter) DeleteEngineTemplate(ctx context.Context, namespace, name string) error {
	if err := validEngineTemplateName(namespace, name); err != nil {
		return err
	}
	for _, path := range []string{engineAPI + namespace + "/agenttemplates/" + name, engineAPI + namespace + "/modelconfigs/" + name + "-model",
		"/api/v1/namespaces/" + namespace + "/secrets/" + name} {
		var existing map[string]any
		status, err := adapter.request(ctx, http.MethodGet, path, nil, &existing)
		if err != nil {
			return err
		}
		if status == http.StatusNotFound {
			continue
		}
		if status != http.StatusOK {
			return apiError("read engine resource", status)
		}
		uid, owned := ownedEngineResource(existing)
		if !owned {
			return errors.New("engine resource is not owned by Marina; refusing to delete it")
		}
		options := map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": map[string]any{"uid": uid}}
		status, err = adapter.request(ctx, http.MethodDelete, path, options, nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK && status != http.StatusAccepted && status != http.StatusNotFound {
			return apiError("delete engine resource", status)
		}
	}
	return nil
}

func validEngineTemplateName(namespace, name string) error {
	// The ModelConfig adds "-model"; both names must remain Kubernetes names.
	if !dnsName.MatchString(namespace) || !dnsName.MatchString(name) || len(name) > 57 || strings.Contains(name, ".") ||
		!strings.HasPrefix(name, runtimeapi.EngineTemplatePrefix) {
		return fmt.Errorf("engine template name must be attempt-exclusive and Marina-owned: %w", runtimeapi.ErrInvalidInput)
	}
	return nil
}

// ensureEngineResource reads before creating so that a lost create response or
// a restart adopts only the resource this exact attempt defined.
func (adapter *Adapter) ensureEngineResource(ctx context.Context, collection string, desired map[string]any, matches func(map[string]any) bool) (map[string]any, error) {
	name := desired["metadata"].(map[string]any)["name"].(string)
	for range 2 {
		var existing map[string]any
		status, err := adapter.request(ctx, http.MethodGet, collection+"/"+name, nil, &existing)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			if _, owned := ownedEngineResource(existing); !owned || !matches(existing) {
				return nil, errors.New("engine resource " + name + " exists with other content; refusing to adopt it")
			}
			return existing, nil
		}
		if status != http.StatusNotFound {
			return nil, apiError("read engine resource", status)
		}
		var created map[string]any
		status, err = adapter.request(ctx, http.MethodPost, collection, desired, &created)
		if err != nil {
			return nil, err
		}
		if status == http.StatusCreated || status == http.StatusOK {
			return created, nil
		}
		if status != http.StatusConflict {
			return nil, apiError("create engine resource", status)
		}
	}
	return nil, errors.New("engine resource " + name + " changed while it was created")
}

func ownedEngineResource(object map[string]any) (string, bool) {
	metadata, _ := object["metadata"].(map[string]any)
	labels, _ := metadata["labels"].(map[string]any)
	uid, _ := metadata["uid"].(string)
	return uid, labels[engineOwnerLabel] == engineOwner && uid != ""
}

func sameSpec(desired map[string]any) func(map[string]any) bool {
	want, _ := json.Marshal(desired["spec"])
	return func(existing map[string]any) bool {
		var normalized any
		_ = json.Unmarshal(want, &normalized)
		return reflect.DeepEqual(normalized, existing["spec"])
	}
}

// preparedRevision accepts only a Ready status for the current generation
// whose latest successful revision is also the desired one.
func preparedRevision(template map[string]any, harness string) (string, error) {
	metadata, _ := template["metadata"].(map[string]any)
	status, _ := template["status"].(map[string]any)
	if generation, ok := metadata["generation"].(float64); ok {
		if observed, _ := status["observedGeneration"].(float64); observed != generation {
			return "", runtimeapi.ErrEngineTemplatePending
		}
	}
	items, _ := status["harnesses"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		revision, _ := item["latestSuccessfulRevision"].(string)
		if item["harness"] != harness || revision == "" || item["desiredRevision"] != revision {
			continue
		}
		conditions, _ := item["conditions"].([]any)
		for _, raw := range conditions {
			condition, _ := raw.(map[string]any)
			if condition["type"] == "Ready" && condition["status"] == "True" {
				if decoded, err := hex.DecodeString(revision); err != nil || len(decoded) != 32 || strings.ToLower(revision) != revision {
					return "", errors.New("engine reported an invalid prepared revision")
				}
				return revision, nil
			}
		}
	}
	return "", runtimeapi.ErrEngineTemplatePending
}
