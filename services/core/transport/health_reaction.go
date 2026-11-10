package transport

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
)

// A browser reaction task records five exploratory response times. It is not
// a clinical measurement, and its timing includes browser and display delay.
func reactionSummary(samples []float64) (median, mean float64, err error) {
	if len(samples) != 5 {
		return 0, 0, errors.New("the reaction task requires exactly five valid trials")
	}
	ordered := append([]float64(nil), samples...)
	for _, value := range ordered {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 80 || value > 1500 {
			return 0, 0, errors.New("reaction times must be between 80 and 1500 milliseconds")
		}
		mean += value
	}
	sort.Float64s(ordered)
	return ordered[2], mean / float64(len(ordered)), nil
}

func (s *Server) uiReactionTest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TrialsMS []float64 `json:"trials_ms"`
	}
	if err := decode(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid reaction test data")
		return
	}
	median, mean, err := reactionSummary(input.TrialsMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := json.Marshal(map[string]any{
		"domain": "cognition",
		"metric": "simple_reaction_ms",
		"value": median,
		"unit": "ms",
		"source": "browser_visual_reaction_test",
		"method": "estimated",
		"metadata": map[string]any{
			"trials_ms": input.TrialsMS,
			"mean_ms": mean,
			"protocol": "five trial visual response",
			"constraints": "Browser and display latency; not clinically standardized",
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot encode reaction test")
		return
	}
	out := s.invoke(r.Context(), "health__health_record_add", raw, json.RawMessage("1"))
	if failed, _ := out["isError"].(bool); failed {
		writeError(w, http.StatusBadGateway, "Health rejected the reaction test")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
