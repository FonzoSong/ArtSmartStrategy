package httpapi

import "testing"

func pointer[T any](value T) *T { return &value }

func TestMergeModelSettingsKeepsUnsentFields(t *testing.T) {
	current := defaultModelSettings("https://api.example/v1", "qwen3.8-max")
	current.MinSample = 12
	current.PlatformWeightsRaw = `{"视频号":1.2}`
	merged, err := mergeModelSettings(current, modelSettingsRequest{Name: pointer("主模型")})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Name != "主模型" || merged.MinSample != 12 || merged.Model != "qwen3.8-max" {
		t.Fatalf("unsent fields must keep their stored value: %#v", merged)
	}
	if merged.PlatformWeightsRaw != `{"视频号":1.2}` {
		t.Fatalf("platform weights must be kept: %s", merged.PlatformWeightsRaw)
	}
}

func TestMergeModelSettingsAcceptsTheSettingsPagePayload(t *testing.T) {
	current := defaultModelSettings("https://api.example/v1", "qwen3.8-max")
	// Exactly what the settings form submits today, including the fields that
	// previously made the request invalid.
	merged, err := mergeModelSettings(current, modelSettingsRequest{
		Name:            pointer("界面模型"),
		Model:           pointer("qwen3-max"),
		BaseURL:         pointer("https://api.example/v1"),
		APIKey:          pointer("secret"),
		Temperature:     pointer(0.3),
		RequestMode:     pointer("base"),
		AuthType:        pointer("bearer"),
		Enabled:         pointer(true),
		FallbackEnabled: pointer(true),
		PlatformWeights: []byte(`{"视频号":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Name != "界面模型" || !merged.Enabled || !merged.FallbackEnabled || merged.PlatformWeightsRaw != `{"视频号":1}` {
		t.Fatalf("settings payload was not applied: %#v", merged)
	}
}

func TestMergeModelSettingsRejectsInvalidValues(t *testing.T) {
	current := defaultModelSettings("https://api.example/v1", "m")
	cases := map[string]modelSettingsRequest{
		"negative sample":     {MinSample: pointer(-1)},
		"weight out of range": {CompletionWeight: pointer(3.0)},
		"temperature range":   {Temperature: pointer(5.0)},
		"bad base url":        {BaseURL: pointer("ftp://example.com")},
	}
	for name, request := range cases {
		if _, err := mergeModelSettings(current, request); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}

func TestSettingsResponseNeverExposesTheKey(t *testing.T) {
	payload := settingsResponse(defaultModelSettings("https://api.example/v1", "m"), true, "已保存的模型密钥")
	if _, leaked := payload["api_key"]; leaked {
		t.Fatal("the stored API key must never be returned")
	}
	if payload["has_key"] != true {
		t.Fatal("the response must state that a key is configured")
	}
}
