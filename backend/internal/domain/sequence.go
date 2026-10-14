package domain

import (
	"fmt"
	"regexp"
	"strconv"
)

type SequenceStep struct {
	Index        int     `json:"index"`
	AccountRole  string  `json:"accountRole"`
	Text         string  `json:"text"`
	DelaySeconds float64 `json:"delaySeconds"`
}
type ResolvedStep struct {
	SequenceStep
	ResolvedVars map[string]string `json:"resolvedVars"`
	VarSources   map[string]string `json:"varSources"`
}
type PlaceholderError struct {
	StepIndex int
	Key       string
}

func (e *PlaceholderError) Error() string {
	return fmt.Sprintf("step %d: unresolved placeholder %s", e.StepIndex, e.Key)
}

var placeholder = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// Resolve is shared by preview and start. Overrides remain active in later steps.
func Resolve(steps []SequenceStep, vars map[string]string, overrides map[string]map[string]string) ([]ResolvedStep, error) {
	values, sources := map[string]string{}, map[string]string{}
	for k, v := range vars {
		if v != "" {
			values[k] = v
			sources[k] = "default"
		}
	}
	result := make([]ResolvedStep, 0, len(steps))
	for _, step := range steps {
		for k, v := range overrides[strconv.Itoa(step.Index)] {
			if v != "" {
				values[k] = v
				sources[k] = "step:" + strconv.Itoa(step.Index)
			}
		}
		r := ResolvedStep{SequenceStep: step, ResolvedVars: map[string]string{}, VarSources: map[string]string{}}
		var missing string
		r.Text = placeholder.ReplaceAllStringFunc(step.Text, func(s string) string {
			k := s[1 : len(s)-1]
			v, ok := values[k]
			if !ok {
				if missing == "" {
					missing = k
				}
				return s
			}
			r.ResolvedVars[k] = v
			r.VarSources[k] = sources[k]
			return v
		})
		if missing != "" {
			return nil, &PlaceholderError{step.Index, missing}
		}
		result = append(result, r)
	}
	return result, nil
}
