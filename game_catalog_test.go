package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type gameCatalogEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	NameZH       string `json:"nameZh"`
	Category     string `json:"category"`
	Source       string `json:"source"`
	Variant      string `json:"variant"`
	Image        string `json:"image"`
	PaldexNumber int    `json:"paldexNumber"`
}

type gameCatalogMetadata struct {
	SchemaVersion             int    `json:"schemaVersion"`
	SteamBuildID              string `json:"steamBuildId"`
	AtlasItemChecksum         string `json:"atlasItemChecksum"`
	AtlasPalChecksum          string `json:"atlasPalChecksum"`
	CompleteItemChecksum      string `json:"completeItemChecksum"`
	CompleteCharacterChecksum string `json:"completeCharacterChecksum"`
	CompleteSkillChecksum     string `json:"completeSkillChecksum"`
	ChineseItemChecksum       string `json:"chineseItemChecksum"`
	ChinesePalChecksum        string `json:"chinesePalChecksum"`
	ChineseSkillChecksum      string `json:"chineseSkillChecksum"`
	OfficialItemCount         int    `json:"officialItemCount"`
	DeveloperItemCount        int    `json:"developerItemCount"`
	LegacyItemCount           int    `json:"legacyItemCount"`
	ItemCount                 int    `json:"itemCount"`
	PalCount                  int    `json:"palCount"`
	BossPalCount              int    `json:"bossPalCount"`
	SkillCount                int    `json:"skillCount"`
	PassiveCount              int    `json:"passiveCount"`
	ItemIDDigest              string `json:"itemIdDigest"`
	PalIDDigest               string `json:"palIdDigest"`
	BossPalIDDigest           string `json:"bossPalIdDigest"`
	SkillIDDigest             string `json:"skillIdDigest"`
	PassiveIDDigest           string `json:"passiveIdDigest"`
}

type gameCatalogFile struct {
	Metadata gameCatalogMetadata         `json:"metadata"`
	Items    map[string]gameCatalogEntry `json:"items"`
	Pals     map[string]gameCatalogEntry `json:"pals"`
	BossPals map[string]gameCatalogEntry `json:"bossPals"`
	Skills   map[string]gameCatalogEntry `json:"skills"`
	Passives map[string]gameCatalogEntry `json:"passives"`
}

func readCatalogFile[T any](t *testing.T, name string, target *T) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("frontend", "src", "data", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

func catalogIDDigest(entries map[string]gameCatalogEntry) string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(ids, "\n"))))
}

func TestGameCatalogMatchesPalworldOnePointZeroServerBuild(t *testing.T) {
	catalog := gameCatalogFile{}
	readCatalogFile(t, "game-catalog.json", &catalog)
	items, pals, bossPals, skills, passives := catalog.Items, catalog.Pals, catalog.BossPals, catalog.Skills, catalog.Passives

	if len(items) != 2467 {
		t.Fatalf("item catalog has %d entries, want 2467", len(items))
	}
	if len(pals) != 289 {
		t.Fatalf("pal catalog has %d entries, want 289", len(pals))
	}
	if len(bossPals) != 289 {
		t.Fatalf("boss pal catalog has %d entries, want 289", len(bossPals))
	}
	if len(skills) != 375 {
		t.Fatalf("skill catalog has %d entries, want 375", len(skills))
	}
	if len(passives) != 488 {
		t.Fatalf("passive catalog has %d entries, want 488", len(passives))
	}
	if items["Accessory_HP_1"].NameZH != "生命吊坠" || items["Wood_WorldTree"].NameZH != "神秘木材" {
		t.Fatalf("item Chinese translations are incomplete: %#v %#v", items["Accessory_HP_1"], items["Wood_WorldTree"])
	}
	if pals["SheepBall"].NameZH != "棉悠悠" || pals["WorldTreeDragon"].NameZH != "枯星龙" {
		t.Fatalf("pal Chinese translations are incomplete: %#v %#v", pals["SheepBall"], pals["WorldTreeDragon"])
	}
	if pals["PlantSlime_Flower"].NameZH != "花叶泥泥" || bossPals["BOSS_PlantSlime_Flower"].NameZH != "花叶泥泥（首领）" {
		t.Fatalf("Pal variant Chinese translations are incomplete: %#v %#v", pals["PlantSlime_Flower"], bossPals["BOSS_PlantSlime_Flower"])
	}

	for _, id := range []string{
		"AncientArmor",
		"AssaultRifle_NPC_GrassBoss",
		"BeamLauncher",
		"WingGlider",
		"Wood_WorldTree",
		"WorldTreeRelic_04",
		"YakushimaBlade003_5",
	} {
		if _, ok := items[id]; !ok {
			t.Errorf("current item %q is missing", id)
		}
	}
	if got := pals["PlantSlime_Flower"].Image; got != "https://cdn.paldb.cc/image/Pal/Texture/PalIcon/Normal/T_PlantSlime_icon_normal.webp" {
		t.Errorf("PlantSlime_Flower icon = %q", got)
	}
	for _, id := range []string{
		"BlackPuppy_Ice",
		"KingWhale",
		"LeafMomonga",
		"LegendDeer",
		"PlantSlime_Flower",
		"PoseidonOrca",
		"WorldTreeDragon",
	} {
		if _, ok := pals[id]; !ok {
			t.Errorf("current pal %q is missing", id)
		}
	}

	tokenPattern := regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	templateTokenPattern := regexp.MustCompile(`^[A-Za-z0-9_.*-]+$`)
	hanPattern := regexp.MustCompile(`[\p{Han}]`)
	officialItems, developerItems, legacyItems := 0, 0, 0
	for key, entry := range items {
		if entry.ID != key || !tokenPattern.MatchString(entry.ID) || entry.Name == "" || !hanPattern.MatchString(entry.NameZH) {
			t.Errorf("invalid item entry %q: %#v", key, entry)
		}
		switch entry.Source {
		case "official":
			officialItems++
		case "developer":
			developerItems++
		case "legacy":
			legacyItems++
		default:
			t.Errorf("item %q has unknown source %q", key, entry.Source)
		}
	}
	if officialItems != 1891 || developerItems != 575 || legacyItems != 1 {
		t.Fatalf("item sources = official:%d developer:%d legacy:%d, want official:1891 developer:575 legacy:1", officialItems, developerItems, legacyItems)
	}
	for key, entry := range pals {
		if entry.ID != key || !tokenPattern.MatchString(entry.ID) || entry.Name == "" || !hanPattern.MatchString(entry.NameZH) {
			t.Errorf("invalid pal entry %q: %#v", key, entry)
		}
		if entry.Source != "official" {
			t.Errorf("pal %q has source %q, want official", key, entry.Source)
		}
	}
	for key, entry := range bossPals {
		if entry.ID != key || !tokenPattern.MatchString(entry.ID) || entry.Name == "" || !hanPattern.MatchString(entry.NameZH) {
			t.Errorf("invalid boss pal entry %q: %#v", key, entry)
		}
		if entry.Source != "official" || entry.Variant != "boss" {
			t.Errorf("boss pal %q has source %q and variant %q", key, entry.Source, entry.Variant)
		}
	}
	if _, ok := bossPals["Boss_Anubis"]; !ok {
		t.Error("verified Boss_Anubis token is missing")
	}
	if _, ok := bossPals["BOSS_Anubis"]; ok {
		t.Error("fabricated BOSS_Anubis token must not be present")
	}
	for label, entries := range map[string]map[string]gameCatalogEntry{"skill": skills, "passive": passives} {
		for key, entry := range entries {
			if entry.ID != key || !templateTokenPattern.MatchString(entry.ID) || entry.Name == "" || !hanPattern.MatchString(entry.NameZH) {
				t.Errorf("invalid %s entry %q: %#v", label, key, entry)
			}
		}
	}

	metadata := catalog.Metadata
	if metadata.SchemaVersion != 4 ||
		metadata.SteamBuildID != "24181105" ||
		metadata.AtlasItemChecksum != "7a48993c911a09a1030a92b9dbe454e3cbcc287b51fc9e3c94fe82d941a6c5a8" ||
		metadata.AtlasPalChecksum != "57fb4bf837061c1160d5f72755152245fe793e1b0073328714efd63c65ba5b47" ||
		metadata.CompleteItemChecksum != "70f242bffca0a64e66e0e91fa8ea1c0709b383c2dbcd4847afb2ff24a5a9ae39" ||
		metadata.CompleteCharacterChecksum != "6ea22f750780ec89fb0ceeef8304335318c0cedffafd47c4a32e2fd85c6e0d39" ||
		metadata.CompleteSkillChecksum != "b9172f389bf56a307194d25b70aca23f8610ef81de32bb44bda827f65b83add1" ||
		metadata.ChineseItemChecksum != "4c16bdc5d727739796fd4a04192ae77c172737ea5ad78b37710924561eb63b57" ||
		metadata.ChinesePalChecksum != "d336740885c9d4378784564532b775e35a356843078bee072e234d227223acfc" ||
		metadata.ChineseSkillChecksum != "49f180c123df6d5d9a5a5c831aaec7d667f57633840da3a927110dbcb5da6911" ||
		metadata.OfficialItemCount != 1891 || metadata.DeveloperItemCount != 575 || metadata.LegacyItemCount != 1 ||
		metadata.ItemCount != len(items) || metadata.PalCount != len(pals) || metadata.BossPalCount != len(bossPals) ||
		metadata.SkillCount != len(skills) || metadata.PassiveCount != len(passives) ||
		metadata.ItemIDDigest != "5097e2947d0fc2c2f6632bc8bc30059bc0955f6be9edb0f42690445bbc6db4a4" ||
		metadata.PalIDDigest != "138c933f7e2f46cb7c8a0e71ea0d57dd23858d2fa72d289bd1281d50ab9c022b" ||
		metadata.BossPalIDDigest != "4c6c87647700becdbd9688a14c938cd48112e37d39a3de31a05ba1db52234980" ||
		metadata.SkillIDDigest != "c3362a462eb75e4c7c977e761785f1ba71ffb6aad4cb8cb9b1d9ec8661d235a5" ||
		metadata.PassiveIDDigest != "7bb93659be2f2bdca77985e205d224663b65d5b68e01978f038bb42335a068bf" ||
		metadata.ItemIDDigest != catalogIDDigest(items) || metadata.PalIDDigest != catalogIDDigest(pals) || metadata.BossPalIDDigest != catalogIDDigest(bossPals) ||
		metadata.SkillIDDigest != catalogIDDigest(skills) || metadata.PassiveIDDigest != catalogIDDigest(passives) {
		t.Fatalf("catalog metadata does not match data: %#v", metadata)
	}
}
