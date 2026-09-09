package flow

import "testing"

func TestConfigurationRequiresExplicitDestinationsAndVerification(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("empty configuration must not enable execution")
	}
	if safeURL("http://public.example/v1") {
		t.Fatal("cleartext remote endpoint accepted")
	}
	if !safeURL("https://model.internal/v1") {
		t.Fatal("private HTTPS endpoint rejected")
	}
}
