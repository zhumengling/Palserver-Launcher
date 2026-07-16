package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPalTemplateIncludesSelectedCustomization(t *testing.T) {
	request := ActionRequest{
		Action: "pal",
		UserID: "steam_76561190000000000",
		Value:  "WorldTreeDragon",
		Amount: 1,
		Pal: &PalGrantOptions{
			Custom:            true,
			Level:             65,
			Gender:            "Female",
			Nickname:          "世界树守护者",
			Shiny:             true,
			PartnerSkillLevel: 5,
			ActiveSkills:      []string{"Unique_WorldTreeDragon_Supernova", "Unique_WorldTreeDragon_HaloBeam"},
			Passives:          []string{"Legend", "MoveSpeed_up_3"},
			IVs:               PalIVs{Health: 100, AttackMelee: 90, AttackShot: 95, Defense: 88},
			PalSouls:          PalSoulBonuses{Health: 30, Attack: 30, Defense: 20, CraftSpeed: 10},
		},
	}

	payload, err := buildPalTemplate(request)
	if err != nil {
		t.Fatal(err)
	}
	if payload.PalID != request.Value || payload.Level != 65 || payload.Gender != "Female" || payload.Nickname != "世界树守护者" || !payload.Shiny {
		t.Fatalf("unexpected PalTemplate basics: %#v", payload)
	}
	if payload.PartnerSkillLevel != 5 || len(payload.ActiveSkills) != 2 || len(payload.Passives) != 2 {
		t.Fatalf("unexpected PalTemplate skills: %#v", payload)
	}
	if payload.IVs == nil || payload.IVs.Health != 100 || payload.PalSouls == nil || payload.PalSouls.Attack != 30 {
		t.Fatalf("unexpected PalTemplate stats: %#v", payload)
	}
}

func TestBuildPalTemplateRejectsUnsafeOrUnsupportedCustomization(t *testing.T) {
	tests := []PalGrantOptions{
		{Custom: true, Level: 256},
		{Custom: true, Level: 1, Gender: "Unknown"},
		{Custom: true, Level: 1, ActiveSkills: []string{"A", "B", "C", "D"}},
		{Custom: true, Level: 1, Passives: []string{"A", "B", "C", "D", "E"}},
		{Custom: true, Level: 1, ActiveSkills: []string{"AirCanon\nShutdown 0"}},
		{Custom: true, Level: 1, Nickname: "bad\nname"},
		{Custom: true, Level: 1, IVs: PalIVs{Health: 256}},
	}
	for _, options := range tests {
		request := ActionRequest{Action: "pal", UserID: "steam_1", Value: "SheepBall", Amount: 1, Pal: &options}
		if payload, err := buildPalTemplate(request); err == nil {
			t.Fatalf("invalid customization was accepted: %#v -> %#v", options, payload)
		}
	}
}

func TestGrantCustomPalWritesUsesAndRemovesTemplate(t *testing.T) {
	root := t.TempDir()
	instance := ServerInstance{RootPath: root, RCONPort: 25575, AdminPassword: "secret"}
	base := win64Path(instance)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "PalDefender.dll"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := ActionRequest{
		Action: "pal",
		UserID: "steam_76561190000000000",
		Value:  "SheepBall",
		Amount: 2,
		Pal:    &PalGrantOptions{Custom: true, Level: 20, Gender: "Male", ActiveSkills: []string{"AirCanon"}},
	}

	commands := []string{}
	var templatePath string
	response, err := grantCustomPalWithSender(instance, request, func(_ ServerInstance, command string) (string, error) {
		commands = append(commands, command)
		parts := strings.Fields(command)
		if len(parts) != 3 || parts[0] != "givepal_j" || parts[1] != request.UserID {
			t.Fatalf("unexpected custom Pal command: %q", command)
		}
		templatePath = filepath.Join(base, "PalDefender", "Pals", "Templates", parts[2]+".json")
		data, readErr := os.ReadFile(templatePath)
		if readErr != nil {
			t.Fatalf("template was not available during RCON command: %v", readErr)
		}
		payload := palTemplatePayload{}
		if decodeErr := json.Unmarshal(data, &payload); decodeErr != nil || payload.PalID != request.Value || payload.Level != 20 {
			t.Fatalf("invalid generated template: %#v, %v", payload, decodeErr)
		}
		return "OK", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if response != "OK\nOK" || len(commands) != 2 {
		t.Fatalf("response=%q commands=%#v", response, commands)
	}
	if _, err := os.Stat(templatePath); !os.IsNotExist(err) {
		t.Fatalf("temporary PalTemplate was not removed: %v", err)
	}
}
