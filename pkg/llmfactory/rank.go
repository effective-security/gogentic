package llmfactory

import (
	"sort"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
)

// rankModels orders model IDs best first: generally available before preview
// stages, higher version first, a floating ID before a dated snapshot and a
// newer snapshot before an older one, fewer name parts first, then
// alphabetically. The input is not modified.
func rankModels(ids []string) []string {
	type ranked struct {
		id   string
		name llms.ModelName
	}
	models := make([]ranked, 0, len(ids))
	for _, id := range ids {
		models = append(models, ranked{
			id:   id,
			name: llms.ParseModelName(id),
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		a, b := models[i].name, models[j].name
		if aStaged, bStaged := a.Stage != "", b.Stage != ""; aStaged != bStaged {
			return !aStaged
		}
		if cmp := llms.CompareVersions(a.Version, b.Version); cmp != 0 {
			return cmp > 0
		}
		if a.Snapshot != b.Snapshot {
			if a.Snapshot == "" || b.Snapshot == "" {
				return a.Snapshot == ""
			}
			return a.Snapshot > b.Snapshot
		}
		if aParts, bParts := nameParts(a.Family), nameParts(b.Family); aParts != bParts {
			return aParts < bParts
		}
		return models[i].id < models[j].id
	})
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.id)
	}
	return out
}

// nameParts counts the dash-separated parts of a family name.
func nameParts(family string) int {
	return strings.Count(family, "-") + 1
}
