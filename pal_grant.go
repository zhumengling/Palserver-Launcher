package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const (
	maxPalGrantCount = 20
	maxPalLevel      = 255
	maxPalStatValue  = 255
)

var palTemplateIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.*-]+$`)

type palTemplateIVs struct {
	Health      int `json:"Health"`
	AttackMelee int `json:"AttackMelee"`
	AttackShot  int `json:"AttackShot"`
	Defense     int `json:"Defense"`
}

type palTemplateSoulBonuses struct {
	Health     int `json:"Health"`
	Attack     int `json:"Attack"`
	Defense    int `json:"Defense"`
	CraftSpeed int `json:"CraftSpeed"`
}

type palTemplatePayload struct {
	PalID             string                  `json:"PalID"`
	Nickname          string                  `json:"Nickname,omitempty"`
	Gender            string                  `json:"Gender,omitempty"`
	Level             int                     `json:"Level"`
	Shiny             bool                    `json:"Shiny,omitempty"`
	PartnerSkillLevel int                     `json:"PartnerSkillLevel"`
	PalSouls          *palTemplateSoulBonuses `json:"PalSouls,omitempty"`
	IVs               *palTemplateIVs         `json:"IVs,omitempty"`
	ActiveSkills      []string                `json:"ActiveSkills,omitempty"`
	Passives          []string                `json:"Passives,omitempty"`
}

type palRCONSender func(ServerInstance, string) (string, error)

func palGrantLevel(request ActionRequest) (int, error) {
	level := 1
	if request.Pal != nil && request.Pal.Level != 0 {
		level = request.Pal.Level
	}
	if level < 1 || level > maxPalLevel {
		return 0, fmt.Errorf("pal level must be between 1 and %d", maxPalLevel)
	}
	return level, nil
}

func validatePalList(label string, values []string, maximum int) error {
	if len(values) > maximum {
		return fmt.Errorf("%s supports at most %d entries", label, maximum)
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		if !palTemplateIDPattern.MatchString(value) {
			return fmt.Errorf("%s contains unsupported characters", label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate ID %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validatePalStat(label string, value int) error {
	if value < 0 || value > maxPalStatValue {
		return fmt.Errorf("%s must be between 0 and %d", label, maxPalStatValue)
	}
	return nil
}

func hasPalIVs(value PalIVs) bool {
	return value.Health != 0 || value.AttackMelee != 0 || value.AttackShot != 0 || value.Defense != 0
}

func hasPalSoulBonuses(value PalSoulBonuses) bool {
	return value.Health != 0 || value.Attack != 0 || value.Defense != 0 || value.CraftSpeed != 0
}

func buildPalTemplate(request ActionRequest) (palTemplatePayload, error) {
	if err := validatePlayerActionToken("palId", request.Value); err != nil {
		return palTemplatePayload{}, err
	}
	if request.Pal == nil || !request.Pal.Custom {
		return palTemplatePayload{}, fmt.Errorf("custom Pal options are required")
	}
	options := request.Pal
	level, err := palGrantLevel(request)
	if err != nil {
		return palTemplatePayload{}, err
	}
	gender := strings.TrimSpace(options.Gender)
	if gender == "Random" {
		gender = ""
	}
	if gender != "" && gender != "Male" && gender != "Female" && gender != "None" {
		return palTemplatePayload{}, fmt.Errorf("unsupported Pal gender %q", options.Gender)
	}
	nickname := strings.TrimSpace(options.Nickname)
	if len([]rune(nickname)) > 64 || strings.IndexFunc(nickname, unicode.IsControl) >= 0 {
		return palTemplatePayload{}, fmt.Errorf("Pal nickname contains unsupported characters or is too long")
	}
	partnerSkillLevel := options.PartnerSkillLevel
	if partnerSkillLevel == 0 {
		partnerSkillLevel = 1
	}
	if partnerSkillLevel < 1 || partnerSkillLevel > maxPalLevel {
		return palTemplatePayload{}, fmt.Errorf("partner skill level must be between 1 and %d", maxPalLevel)
	}
	if err := validatePalList("activeSkills", options.ActiveSkills, 3); err != nil {
		return palTemplatePayload{}, err
	}
	if err := validatePalList("passives", options.Passives, 4); err != nil {
		return palTemplatePayload{}, err
	}
	for label, value := range map[string]int{
		"IV health": options.IVs.Health, "IV melee attack": options.IVs.AttackMelee,
		"IV ranged attack": options.IVs.AttackShot, "IV defense": options.IVs.Defense,
		"soul health": options.PalSouls.Health, "soul attack": options.PalSouls.Attack,
		"soul defense": options.PalSouls.Defense, "soul craft speed": options.PalSouls.CraftSpeed,
	} {
		if err := validatePalStat(label, value); err != nil {
			return palTemplatePayload{}, err
		}
	}

	payload := palTemplatePayload{
		PalID: request.Value, Nickname: nickname, Gender: gender, Level: level, Shiny: options.Shiny,
		PartnerSkillLevel: partnerSkillLevel, ActiveSkills: append([]string(nil), options.ActiveSkills...),
		Passives: append([]string(nil), options.Passives...),
	}
	if hasPalIVs(options.IVs) {
		payload.IVs = &palTemplateIVs{
			Health: options.IVs.Health, AttackMelee: options.IVs.AttackMelee,
			AttackShot: options.IVs.AttackShot, Defense: options.IVs.Defense,
		}
	}
	if hasPalSoulBonuses(options.PalSouls) {
		payload.PalSouls = &palTemplateSoulBonuses{
			Health: options.PalSouls.Health, Attack: options.PalSouls.Attack,
			Defense: options.PalSouls.Defense, CraftSpeed: options.PalSouls.CraftSpeed,
		}
	}
	return payload, nil
}

func newPalTemplateName() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "palserver_launcher_" + hex.EncodeToString(random), nil
}

func grantCustomPalWithSender(instance ServerInstance, request ActionRequest, sender palRCONSender) (string, error) {
	if err := validatePlayerActionToken("userId", request.UserID); err != nil {
		return "", err
	}
	if request.Amount < 1 || request.Amount > maxPalGrantCount {
		return "", fmt.Errorf("Pal grant count must be between 1 and %d", maxPalGrantCount)
	}
	base := win64Path(instance)
	if _, err := os.Stat(filepath.Join(base, "PalDefender.dll")); err != nil {
		return "", fmt.Errorf("PalDefender is not installed or enabled: %w", err)
	}
	payload, err := buildPalTemplate(request)
	if err != nil {
		return "", err
	}
	name, err := newPalTemplateName()
	if err != nil {
		return "", err
	}
	templateDirectory := filepath.Join(base, "PalDefender", "Pals", "Templates")
	if err := os.MkdirAll(templateDirectory, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	templatePath := filepath.Join(templateDirectory, name+".json")
	if err := replaceFileData(templatePath, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	defer os.Remove(templatePath)

	responses := make([]string, 0, request.Amount)
	command := fmt.Sprintf("givepal_j %s %s", request.UserID, name)
	for range request.Amount {
		response, sendErr := sender(instance, command)
		if sendErr != nil {
			return "", sendErr
		}
		responses = append(responses, response)
	}
	return strings.Join(responses, "\n"), nil
}

func grantCustomPal(instance ServerInstance, request ActionRequest) (string, error) {
	return grantCustomPalWithSender(instance, request, sendRCON)
}
