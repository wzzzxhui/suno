package model

import "testing"

func TestGenerateCatalogCoversAllKnownVersions(t *testing.T) {
	want := map[string]string{
		"chirp-hawk":      "V6",
		"chirp-hawk-wild": "V6-wild",
		"chirp-goose":     "V6-mini",
		"chirp-fenix":     "V5.5",
		"chirp-crow":      "V5",
		"chirp-bluejay":   "V4.5+",
		"chirp-auk":       "V4.5",
		"chirp-v4":        "V4",
		"chirp-v3-5":      "V3.5",
		"chirp-v3-0":      "V3",
	}

	got := make(map[string]string)
	for _, m := range ModelsFor(UsageGenerate) {
		got[m.Code] = m.Version
	}

	for code, version := range want {
		if got[code] != version {
			t.Errorf("生成模型 %s 应为版本 %s，实际 %q", code, version, got[code])
		}
	}
	if len(got) != len(want) {
		t.Errorf("生成模型数量应为 %d，实际 %d", len(want), len(got))
	}
}

func TestRemasterCatalog(t *testing.T) {
	want := []string{"chirp-halibut", "chirp-flounder", "chirp-carp", "chirp-bass", "chirp-up"}
	models := ModelsFor(UsageRemaster)
	if len(models) != len(want) {
		t.Fatalf("Remaster 模型数量应为 %d，实际 %d", len(want), len(models))
	}
	for i, code := range want {
		if models[i].Code != code {
			t.Errorf("第 %d 个 Remaster 模型应为 %s，实际 %s", i, code, models[i].Code)
		}
	}
}

func TestNormalizeModelAliases(t *testing.T) {
	cases := []struct {
		usage ModelUsage
		in    string
		want  string
		ok    bool
	}{
		{UsageGenerate, "chirp-hawk", "chirp-hawk", true},
		{UsageGenerate, "chirp-crow", "chirp-crow", true},
		{UsageGenerate, " chirp-auk ", "chirp-auk", true},
		{UsageGenerate, "chirp-v3.5", "chirp-v3-5", true}, // 点号写法折算成标准代号
		{UsageGenerate, "chirp-v5", "chirp-crow", true},   // 版本号写法折算成代号
		{UsageGenerate, "chirp-v4-5-plus", "chirp-bluejay", true},
		{UsageGenerate, "chirp-v6", "chirp-hawk", true},
		{UsageGenerate, "gpt-4", "gpt-4", false},
		{UsageGenerate, "", "", false},
		{UsageSound, "chirp-crow", "chirp-crow", true},
		{UsageSound, "chirp-hawk", "chirp-hawk", false}, // 音效不支持 V6 代号
		{UsageRemaster, "chirp-halibut", "chirp-halibut", true},
		{UsageRemaster, "chirp-hawk", "chirp-hawk", false},
	}

	for _, tc := range cases {
		got, ok := NormalizeModel(tc.usage, tc.in)
		if ok != tc.ok {
			t.Errorf("NormalizeModel(%s, %q) 支持性应为 %v，实际 %v", tc.usage, tc.in, tc.ok, ok)
		}
		if ok && got != tc.want {
			t.Errorf("NormalizeModel(%s, %q) 应折算为 %s，实际 %s", tc.usage, tc.in, tc.want, got)
		}
	}
}

func TestDefaultModel(t *testing.T) {
	cases := map[ModelUsage]string{
		UsageGenerate: "chirp-hawk",
		UsageSound:    "chirp-crow",
		UsageRemaster: "chirp-halibut",
	}
	for usage, want := range cases {
		if got := DefaultModel(usage); got != want {
			t.Errorf("%s 的默认模型应为 %s，实际 %s", usage, want, got)
		}
	}
}

func TestRegisterModelsAddsAndOverrides(t *testing.T) {
	// 用后恢复，避免污染其他用例
	original := extraModels
	defer func() { extraModels = original }()
	extraModels = map[ModelUsage][]MusicModel{}

	before := len(ModelsFor(UsageGenerate))

	RegisterModels(UsageGenerate, []MusicModel{
		{Code: "chirp-newbird", Version: "V7", Label: "Suno V7"},
	})

	if got := len(ModelsFor(UsageGenerate)); got != before+1 {
		t.Fatalf("注册后模型数量应为 %d，实际 %d", before+1, got)
	}
	code, ok := NormalizeModel(UsageGenerate, "chirp-newbird")
	if !ok || code != "chirp-newbird" {
		t.Error("新注册的模型应当通过校验")
	}

	// 未指定状态时补默认值
	for _, m := range ModelsFor(UsageGenerate) {
		if m.Code == "chirp-newbird" && m.Status != StatusActive {
			t.Errorf("新注册模型状态应默认为 active，实际 %s", m.Status)
		}
	}

	// 同 code 再注册应覆盖而非重复
	RegisterModels(UsageGenerate, []MusicModel{
		{Code: "chirp-newbird", Version: "V7.1", Label: "Suno V7.1"},
	})
	if got := len(ModelsFor(UsageGenerate)); got != before+1 {
		t.Errorf("重复注册不应增加数量，实际 %d", got)
	}
	for _, m := range ModelsFor(UsageGenerate) {
		if m.Code == "chirp-newbird" && m.Version != "V7.1" {
			t.Errorf("重复注册应覆盖，版本实际为 %s", m.Version)
		}
	}
}

func TestModelCodes(t *testing.T) {
	codes := ModelCodes(UsageSound)
	if len(codes) != 2 || codes[0] != "chirp-crow" {
		t.Errorf("音效代号列表不符：%v", codes)
	}
}
