package contentpack

import (
	"encoding/json"
	"strings"

	"github.com/caelis-labs/caelis-bot/resources"
)

type figure struct {
	model, avatar string
}

type builtinFigure struct {
	choice Choice
	figure
	hidden bool
}

// The embedded manifest is verified at build time. Models and portraits live once
// in the packaged frontend. The persisted default key predates costume selection.
var builtinFigures = readBuiltinFigures(resources.CharacterPack)

func readBuiltinFigures(manifest string) []builtinFigure {
	var pack struct {
		DefaultCharacter, DefaultVariant, FallbackModel string
		Characters                                      []struct {
			ID, Name string
			Hidden   bool
			Variants []struct {
				ID, Model, Avatar string
			}
		}
	}
	if err := json.Unmarshal([]byte(manifest), &pack); err != nil {
		panic(err)
	}
	publicURL := func(path string) string { return strings.TrimPrefix(path, "frontend/public") }
	var figures []builtinFigure
	for _, character := range pack.Characters {
		for _, variant := range character.Variants {
			name := character.Name
			if name == "" { // Built-in contracts v1–v3 have only a character ID.
				name = character.ID
				name = strings.ToUpper(name[:1]) + name[1:]
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
			figures = append(figures, builtinFigure{Choice{id, name, "", "内置"}, figure{model, publicURL(variant.Avatar)}, character.Hidden})
		}
	}
	// Recognize the former recovery-model choice on profile upgrade.
	return append(figures, builtinFigure{Choice{"builtin:stick", "Stick", "", "内置"}, figure{model: publicURL(pack.FallbackModel)}, true})
}

// Resolve retired selections at both admission and startup. Keep the original
// saved bytes on startup so the profile remains readable by older releases.
func resolveBuiltinSelection(s Selection) Selection {
	for _, builtin := range builtinFigures {
		if builtin.hidden && builtin.choice.ID == s.Character {
			return defaultSelection()
		}
	}
	return s
}
