package care

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"time"
)

// Source describes an application-registered data adapter. Registration belongs
// to the host, not model input. Adapters own collection, JSON/schema validation,
// credentials, cancellation and execution permissions; CEL only sees their data.
type Source struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"fields"`
}

var sourceName = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(\.[a-zA-Z][a-zA-Z0-9]*)+$`)

func NativeSources() []Source {
	return []Source{
		{Name: "clock.minute", Description: "One current wall-clock minute while the app runs; no missed-minute replay. Use local fields for the rule timezone.", Fields: map[string]string{}},
		{Name: "desktop.usage", Description: "Sampled about every 30 seconds with known unlocked presence. Active time resets after a five-minute break, lock, sleep or app restart.", Fields: map[string]string{"activeSeconds": "integer", "idleSeconds": "integer", "application": "bundle identifier string"}},
		{Name: "desktop.appChanged", Description: "A change between sampled foreground application bundle identifiers, with known unlocked presence.", Fields: map[string]string{"application": "bundle identifier string", "previousApplication": "bundle identifier string"}},
	}
}
func registry(sources []Source) (map[string]Source, error) {
	if len(sources) > 32 {
		return nil, errors.New("at most 32 data sources")
	}
	out := map[string]Source{}
	for _, s := range sources {
		if !sourceName.MatchString(s.Name) || len(s.Name) > 128 || s.Description == "" || len(s.Description) > 2048 || len(s.Fields) > 64 {
			return nil, errors.New("invalid source declaration")
		}
		if _, ok := out[s.Name]; ok {
			return nil, errors.New("duplicate source declaration")
		}
		fields := map[string]string{}
		for k, v := range s.Fields {
			if len(k) > 128 || len(v) > 256 {
				return nil, errors.New("invalid source field")
			}
			fields[k] = v
		}
		s.Fields = fields
		out[s.Name] = s
	}
	return out, nil
}
func (e *Engine) Sources() []Source {
	out := make([]Source, 0, len(e.sources))
	for _, s := range e.sources {
		fields := map[string]string{}
		for k, v := range s.Fields {
			fields[k] = v
		}
		s.Fields = fields
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Source) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out
}
func (e *Engine) hasSource(name string) bool { _, ok := e.sources[name]; return ok }
func (e *Engine) Test(ctx context.Context, r Rule, data map[string]any, now time.Time) (bool, error) {
	if !e.hasSource(r.On) {
		return false, errors.New("unsupported event source")
	}
	return Test(ctx, r, data, now)
}
