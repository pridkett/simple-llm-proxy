package provider

import "testing"

func TestDeploymentKey(t *testing.T) {
	t.Run("empty api_base keeps trailing colon", func(t *testing.T) {
		d := &Deployment{
			ProviderName: "openai",
			ActualModel:  "gpt-4",
			APIBase:      "",
		}
		got := d.DeploymentKey()
		want := "openai:gpt-4:"
		if got != want {
			t.Errorf("DeploymentKey() = %q, want %q", got, want)
		}
	})

	t.Run("anthropic with empty api_base", func(t *testing.T) {
		d := &Deployment{
			ProviderName: "anthropic",
			ActualModel:  "claude-3-sonnet",
			APIBase:      "",
		}
		got := d.DeploymentKey()
		want := "anthropic:claude-3-sonnet:"
		if got != want {
			t.Errorf("DeploymentKey() = %q, want %q", got, want)
		}
	})

	t.Run("non-empty api_base included in key", func(t *testing.T) {
		d := &Deployment{
			ProviderName: "openai",
			ActualModel:  "gpt-4",
			APIBase:      "https://api.example.com",
		}
		got := d.DeploymentKey()
		want := "openai:gpt-4:https://api.example.com"
		if got != want {
			t.Errorf("DeploymentKey() = %q, want %q", got, want)
		}
	})

	t.Run("identical fields produce identical keys", func(t *testing.T) {
		d1 := &Deployment{ProviderName: "openai", ActualModel: "gpt-4", APIBase: ""}
		d2 := &Deployment{ProviderName: "openai", ActualModel: "gpt-4", APIBase: ""}
		if d1.DeploymentKey() != d2.DeploymentKey() {
			t.Errorf("expected identical keys, got %q and %q", d1.DeploymentKey(), d2.DeploymentKey())
		}
	})

	t.Run("different api_base produces different keys", func(t *testing.T) {
		d1 := &Deployment{ProviderName: "openai", ActualModel: "gpt-4", APIBase: ""}
		d2 := &Deployment{ProviderName: "openai", ActualModel: "gpt-4", APIBase: "https://api.example.com"}
		if d1.DeploymentKey() == d2.DeploymentKey() {
			t.Errorf("expected different keys for different api_base, got %q for both", d1.DeploymentKey())
		}
	})
}
