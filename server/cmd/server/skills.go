package main

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cleiderg/lush/server/internal/world"
)

// skillsData is a copy of shared/map/skills.json, embedded so the deployed
// binary needs no extra file. TestEmbeddedSkillsMatchShared keeps it in sync;
// refresh with: cp shared/map/skills.json server/cmd/server/skills.json
//
//go:embed skills.json
var skillsData []byte

// bankBooth returns the position of the bank booth in the embedded skills
// data, in metres. The file's units are decimetres.
func bankBooth() (world.Vec2, error) {
	var sk struct {
		Units string `json:"units"`
		Bank  *struct {
			X float32 `json:"x"`
			Z float32 `json:"z"`
		} `json:"bank"`
	}
	if err := json.Unmarshal(skillsData, &sk); err != nil {
		return world.Vec2{}, fmt.Errorf("embedded skills data: %w", err)
	}
	if sk.Units != "decimetres" || sk.Bank == nil {
		return world.Vec2{}, fmt.Errorf("embedded skills data: want decimetres units and a bank booth, got units %q", sk.Units)
	}
	return world.Vec2{X: sk.Bank.X / 10, Y: sk.Bank.Z / 10}, nil
}
