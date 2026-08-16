package signals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var ErrInvalidSignal = errors.New("invalid signal")

const TrustValidatedObservation = "validated_observation"

type RawSignal struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	ExternalID string            `json:"external_id"`
	ProjectID  string            `json:"project_id"`
	Type       string            `json:"type"`
	ObservedAt time.Time         `json:"observed_at"`
	ReceivedAt time.Time         `json:"received_at"`
	Severity   string            `json:"severity"`
	Confidence float64           `json:"confidence"`
	Labels     map[string]string `json:"labels"`
	Text       string            `json:"text"`
}

type SourceRule struct {
	Types           []string          `json:"types"`
	Classifications map[string]string `json:"classifications"`
}

type Configuration struct {
	MaxAge            time.Duration
	MaxRawBytes       int
	CorrelationLabels []string
	Sources           map[string]SourceRule
}

type Validator struct {
	configuration Configuration
}

func NewValidator(configuration Configuration) (*Validator, error) {
	if configuration.MaxAge <= 0 || configuration.MaxRawBytes < 1 || len(configuration.Sources) == 0 ||
		len(configuration.CorrelationLabels) == 0 {
		return nil, ErrInvalidSignal
	}
	for source, rule := range configuration.Sources {
		if !safeIdentifier(source) || len(rule.Types) == 0 {
			return nil, ErrInvalidSignal
		}
		for _, signalType := range rule.Types {
			if !safeIdentifier(signalType) || !safeIdentifier(rule.Classifications[signalType]) {
				return nil, ErrInvalidSignal
			}
		}
	}
	return &Validator{configuration: configuration}, nil
}

type ValidatedSignal struct {
	ID                 string            `json:"id"`
	RawSignalID        string            `json:"raw_signal_id"`
	ProjectID          string            `json:"project_id"`
	Source             string            `json:"source"`
	Type               string            `json:"type"`
	Classification     string            `json:"classification"`
	Severity           string            `json:"severity"`
	Confidence         float64           `json:"confidence"`
	CorrelationID      string            `json:"correlation_id"`
	DedupeKey          string            `json:"dedupe_key"`
	ObservationSummary string            `json:"observation_summary"`
	ValidatedLabels    map[string]string `json:"validated_labels"`
	Trust              string            `json:"trust"`
	ObservedAt         time.Time         `json:"observed_at"`
	ValidatedAt        time.Time         `json:"validated_at"`
}

func (validator *Validator) Validate(now time.Time, raw RawSignal) (ValidatedSignal, error) {
	now = now.UTC()
	raw.ID = strings.TrimSpace(raw.ID)
	raw.Source = strings.TrimSpace(raw.Source)
	raw.ExternalID = strings.TrimSpace(raw.ExternalID)
	raw.ProjectID = strings.TrimSpace(raw.ProjectID)
	raw.Type = strings.TrimSpace(raw.Type)
	raw.Severity = strings.TrimSpace(raw.Severity)
	rule, knownSource := validator.configuration.Sources[raw.Source]
	if raw.ID == "" || raw.ExternalID == "" || !safeIdentifier(raw.ProjectID) || !knownSource ||
		!slices.Contains(rule.Types, raw.Type) || !safeIdentifier(raw.Severity) || raw.Confidence < 0 || raw.Confidence > 1 ||
		raw.ObservedAt.IsZero() || raw.ReceivedAt.IsZero() || raw.ObservedAt.After(raw.ReceivedAt) || raw.ReceivedAt.After(now) ||
		now.Sub(raw.ObservedAt.UTC()) > validator.configuration.MaxAge || len([]byte(raw.Text)) > validator.configuration.MaxRawBytes {
		return ValidatedSignal{}, ErrInvalidSignal
	}
	labels := make(map[string]string, len(validator.configuration.CorrelationLabels))
	for _, label := range validator.configuration.CorrelationLabels {
		value := strings.TrimSpace(raw.Labels[label])
		if !safeIdentifier(label) || !safeIdentifier(value) {
			return ValidatedSignal{}, fmt.Errorf("correlation label %q is missing or unsafe: %w", label, ErrInvalidSignal)
		}
		labels[label] = value
	}
	correlationID, err := contentID("cor", struct {
		ProjectID string            `json:"project_id"`
		Labels    map[string]string `json:"labels"`
	}{raw.ProjectID, labels})
	if err != nil {
		return ValidatedSignal{}, err
	}
	dedupeKey, err := contentID("dedupe", struct {
		ProjectID     string            `json:"project_id"`
		Source        string            `json:"source"`
		Type          string            `json:"type"`
		Severity      string            `json:"severity"`
		Confidence    float64           `json:"confidence"`
		Labels        map[string]string `json:"labels"`
		Text          string            `json:"text"`
		ObservedAtUTC time.Time         `json:"observed_at"`
	}{raw.ProjectID, raw.Source, raw.Type, raw.Severity, raw.Confidence, labels, raw.Text, raw.ObservedAt.UTC()})
	if err != nil {
		return ValidatedSignal{}, err
	}
	classification := rule.Classifications[raw.Type]
	return ValidatedSignal{
		ID: "sig_" + strings.TrimPrefix(dedupeKey, "dedupe_"), RawSignalID: raw.ID,
		ProjectID: raw.ProjectID, Source: raw.Source, Type: raw.Type, Classification: classification,
		Severity: raw.Severity, Confidence: raw.Confidence, CorrelationID: correlationID, DedupeKey: dedupeKey,
		ObservationSummary: fmt.Sprintf("%s observation classified as %s with %s severity", raw.Type, classification, raw.Severity),
		ValidatedLabels:    labels, Trust: TrustValidatedObservation, ObservedAt: raw.ObservedAt.UTC(), ValidatedAt: now,
	}, nil
}

type DiagnosticIntent struct {
	SignalID           string
	CorrelationID      string
	Summary            string
	Authority          []string
	MutationAuthorized bool
}

func ProposeDiagnosticWork(signal ValidatedSignal) (DiagnosticIntent, error) {
	if signal.ID == "" || signal.CorrelationID == "" || signal.Trust != TrustValidatedObservation {
		return DiagnosticIntent{}, ErrInvalidSignal
	}
	return DiagnosticIntent{
		SignalID: signal.ID, CorrelationID: signal.CorrelationID, Summary: signal.ObservationSummary,
		Authority: []string{}, MutationAuthorized: false,
	}, nil
}

func contentID(prefix string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode signal identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return prefix + "_" + hex.EncodeToString(digest[:16]), nil
}

func safeIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._:/-", character) {
			continue
		}
		return false
	}
	return true
}
