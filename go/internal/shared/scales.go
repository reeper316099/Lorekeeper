package shared

import "fmt"

type Scale struct {
	StatusLabels map[Status]string `json:"statusLabels"`
	// ScoreLabels index 0 = score 1.
	ScoreLabels []string `json:"scoreLabels"`
}

type Scales struct {
	Reading  Scale `json:"reading"`
	Watching Scale `json:"watching"`
}

func numbered(set [10]string) []string {
	out := make([]string, 10)
	for i, l := range set {
		out[i] = fmt.Sprintf("%d - %s", i+1, l)
	}
	return out
}

// DefaultScales returns a fresh copy of the default status and score labels.
func DefaultScales() Scales {
	return Scales{
		Reading: Scale{
			StatusLabels: map[Status]string{
				StatusPlanned:    "Plan to Read",
				StatusInProgress: "Reading",
				StatusCompleted:  "Completed",
				StatusOnHold:     "On Hold",
				StatusDropped:    "Dropped",
				StatusRepeating:  "Re-Reading",
			},
			ScoreLabels: numbered(ScoreLabelSets.ReadingNewer),
		},
		Watching: Scale{
			StatusLabels: map[Status]string{
				StatusPlanned:    "Plan to Watch",
				StatusInProgress: "Watching",
				StatusCompleted:  "Finished",
				StatusOnHold:     "On Hold",
				StatusDropped:    "Dropped",
				StatusRepeating:  "Re-Watching",
			},
			ScoreLabels: numbered(ScoreLabelSets.Watching),
		},
	}
}

func (s Scales) forLibrary(l Library) Scale {
	if l == LibraryReading {
		return s.Reading
	}
	return s.Watching
}

// StatusLabel is the display label for a status ("" for nil).
func StatusLabel(s Scales, l Library, status *Status) string {
	if status == nil {
		return ""
	}
	return s.forLibrary(l).StatusLabels[*status]
}

// ScoreLabel is the display label for a score ("" for nil/0, the number when no label exists).
func ScoreLabel(s Scales, l Library, score *int) string {
	if score == nil || *score == 0 {
		return ""
	}
	labels := s.forLibrary(l).ScoreLabels
	if *score >= 1 && *score <= len(labels) {
		return labels[*score-1]
	}
	return fmt.Sprint(*score)
}
