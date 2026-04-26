package config

import (
	"reflect"
	"testing"
)

func TestDeepseekPreset(t *testing.T) {
	p := Presets()
	d, ok := p["deepseek"]
	if !ok {
		t.Fatal("missing deepseek preset")
	}
	if d.Wire != "anthropic" {
		t.Errorf("wire: %q", d.Wire)
	}
	if d.BaseURL != "https://api.deepseek.com/anthropic" {
		t.Errorf("base: %q", d.BaseURL)
	}
	if d.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("env: %q", d.APIKeyEnv)
	}
	if d.DefaultModel != "deepseek-v4-pro" {
		t.Errorf("default: %q", d.DefaultModel)
	}
	want := []string{"deepseek-v4-pro", "deepseek-v4-flash"}
	if !reflect.DeepEqual(d.Models, want) {
		t.Errorf("models: %v", d.Models)
	}
}
