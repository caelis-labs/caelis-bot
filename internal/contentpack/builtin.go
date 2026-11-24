package contentpack

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/resources"
)

type figure struct {
	model, avatar string
	basic         bool
}

type builtinFigure struct {
	choice Choice
	figure
}

// The embedded manifest is verified at build time. Models and portraits live once
// in the packaged frontend. The persisted default key predates costume selection.
var builtinFigures = readBuiltinFigures()

func readBuiltinFigures() []builtinFigure {
	var pack struct {
		DefaultCharacter, DefaultVariant, FallbackModel string
		Characters                                      []struct {
			ID, Name string
			Variants []struct {
				ID, Model, Avatar string
				Capabilities      []string
			}
		}
	}
	if err := json.Unmarshal([]byte(resources.CharacterPack), &pack); err != nil {
		panic(err)
	}
	publicURL := func(path string) string { return strings.TrimPrefix(path, "frontend/public") }
	var figures []builtinFigure
	for _, character := range pack.Characters {
		for _, variant := range character.Variants {
			name := character.Name
			if name == "" { // Built-in contracts v1–v3 have only a character ID.
				name = character.ID
				if character.ID == "caelis" {
					name = "Caelis"
				}
			}
			if len(character.Variants) > 1 {
				name += " · " + variant.ID
			}
			id := "builtin:" + character.ID + "/" + variant.ID
			model := publicURL(variant.Model)
			if character.ID == pack.DefaultCharacter && variant.ID == pack.DefaultVariant {
				id = defaultSelection().Character
				model = "" // The renderer resolves the default and its animated portrait from this manifest.
			}
			figures = append(figures, builtinFigure{Choice{id, name, "", "内置"}, figure{model, publicURL(variant.Avatar), slices.Contains(variant.Capabilities, "basic-3d-v1")}})
		}
	}
	// Older saved selections can still resolve the hidden recovery model.
	return append(figures, builtinFigure{Choice{"builtin:stick", "Stick", "", "内置"}, figure{model: publicURL(pack.FallbackModel), basic: true}})
}
