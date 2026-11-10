package llmfactory

import (
	"sort"
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
)

// rankModels orders model IDs best first: a model in normal service before a
// deprecated one, generally available before preview stages, higher version
// first, a floating ID before a dated snapshot and a newer snapshot before an
// older one, fewer name parts first, then alphabetically. The input is not
// modified.
//
// list tells which models are deprecated; without one, none is.
func rankModels(ids []string, list *modelList) []string {
	type ranked struct {
		id         string
		name       llms.ModelName
		deprecated bool
	}
	models := make([]ranked, 0, len(ids))
	for _, id := range ids {
		models = append(models, ranked{
			id:         id,
			name:       llms.ParseModelName(id),
			deprecated: list.lifecycle(id) == llms.ModelLifecycleDeprecated,
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		a, b := models[i].name, models[j].name
		if models[i].deprecated != models[j].deprecated {
			return !models[i].deprecated
		}
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
