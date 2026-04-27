package tui

import "testing"

func TestVisionSupportedAnthropic(t *testing.T) {
	if !visionSupported("anthropic", "claude-sonnet-4-5") {
		t.Error("claude-sonnet-4-5 should be vision-capable")
	}
	if visionSupported("anthropic", "deepseek-v3") {
		t.Error("deepseek-v3 on anthropic wire should not be vision-capable")
	}
}

func TestVisionSupportedOpenAI(t *testing.T) {
	if !visionSupported("openai", "gpt-4o") {
		t.Error("gpt-4o should be vision-capable")
	}
	if visionSupported("openai", "gpt-3.5-turbo") {
		t.Error("gpt-3.5-turbo should not be vision-capable")
	}
}

func TestVisionSupportedUnknownWireRefuses(t *testing.T) {
	if visionSupported("grpc", "any-model") {
		t.Error("unknown wire must be refused")
	}
}

func TestVisionCapableModelsHasEntries(t *testing.T) {
	models := visionCapableModels()
	if len(models) == 0 {
		t.Fatal("vision-capable list is empty")
	}
	// At least one Anthropic and one OpenAI entry expected.
	var sawAnth, sawOAI bool
	for _, m := range models {
		if m == "gpt-4o" {
			sawOAI = true
		}
		if m == "claude-sonnet-4-5" {
			sawAnth = true
		}
	}
	if !sawAnth || !sawOAI {
		t.Errorf("expected anthropic + openai entries, got %v", models)
	}
}
