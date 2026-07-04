// Package bench measures click-grounding accuracy: given a screenshot and a
// natural-language instruction ("click the Submit button"), does the system
// under test produce a point inside the target element's bounding box? This
// is the standard ScreenSpot-style protocol used to score grounding models.
//
// The system under test is abstracted behind Pointer so the same harness
// scores a raw model, a grounder-backed clicker (see GrounderPointer), or
// anything else that can answer "where do I click" (see FuncPointer).
package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gnanam1990/argus/pkg/action"
)

// Case is one ScreenSpot-style grounding example: a screenshot, a
// natural-language instruction, and the ground-truth bounding box the
// predicted point must land inside.
type Case struct {
	Image       string      `json:"image"`
	Instruction string      `json:"instruction"`
	Box         action.Rect `json:"box"`
}

// rawCase and rawDataset mirror the on-disk JSON shape, where a box is a flat
// [x1,y1,x2,y2] array rather than the action.Rect struct Case exposes.
type rawCase struct {
	Image       string `json:"image"`
	Instruction string `json:"instruction"`
	Box         []int  `json:"box"`
}

type rawDataset struct {
	Cases []rawCase `json:"cases"`
}

// LoadDataset reads the dataset manifest at <dir>/cases.json:
//
//	{"cases": [
//	  {"image": "file.png", "instruction": "click the Submit button", "box": [x1,y1,x2,y2]}
//	]}
//
// Image paths are resolved relative to dir. Every case is validated: the
// image file must exist (and be a regular file), and the box must be exactly
// 4 numbers with the max corner strictly greater than the min corner on both
// axes. A malformed case fails the whole load with an error identifying the
// case index and the problem; LoadDataset never returns a partial dataset.
func LoadDataset(dir string) ([]Case, error) {
	path := filepath.Join(dir, "cases.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("bench: read dataset %s: %w", path, err)
	}
	var doc rawDataset
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("bench: parse dataset %s: %w", path, err)
	}

	cases := make([]Case, 0, len(doc.Cases))
	for i, rc := range doc.Cases {
		if rc.Image == "" {
			return nil, fmt.Errorf("bench: case %d: empty image filename", i)
		}
		imgPath := filepath.Join(dir, rc.Image)
		info, err := os.Stat(imgPath)
		if err != nil {
			return nil, fmt.Errorf("bench: case %d: image %q: %w", i, rc.Image, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("bench: case %d: image %q is a directory, not a file", i, rc.Image)
		}
		if len(rc.Box) != 4 {
			return nil, fmt.Errorf("bench: case %d: box must have 4 numbers [x1,y1,x2,y2], got %d", i, len(rc.Box))
		}
		box := action.Rect{
			Min: action.Point{X: rc.Box[0], Y: rc.Box[1]},
			Max: action.Point{X: rc.Box[2], Y: rc.Box[3]},
		}
		if box.Empty() {
			return nil, fmt.Errorf("bench: case %d: malformed box %v: max must be greater than min on both axes", i, rc.Box)
		}
		cases = append(cases, Case{Image: rc.Image, Instruction: rc.Instruction, Box: box})
	}
	return cases, nil
}

// CaseResult records the outcome of running one Case through a Pointer.
type CaseResult struct {
	Image       string       `json:"image"`
	Instruction string       `json:"instruction"`
	Hit         bool         `json:"hit"`
	Point       action.Point `json:"point"`
	// Err holds the error message when the case could not be scored (image
	// read failure or a Pointer error). Such a case counts as a miss.
	Err string `json:"err,omitempty"`
}

// Report aggregates CaseResults across a dataset run.
type Report struct {
	Total    int          `json:"total"`
	Hits     int          `json:"hits"`
	Accuracy float64      `json:"accuracy"`
	Results  []CaseResult `json:"results"`
}

// JSON renders the report as indented JSON.
func (r Report) JSON() []byte {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return b
}

// Run loads the dataset at dir and, for each case, reads its image and asks p
// to predict a click point for the case's instruction. A predicted point
// inside the case's Box (inclusive edges) counts as a hit. An error reading a
// case's image, or an error from p, is recorded on that case's
// CaseResult.Err and counted as a miss; Run continues with the remaining
// cases so one bad case never aborts the benchmark. Run fails outright only
// when the dataset itself cannot be loaded.
func Run(ctx context.Context, dir string, p Pointer) (Report, error) {
	cases, err := LoadDataset(dir)
	if err != nil {
		return Report{}, err
	}

	rep := Report{Results: make([]CaseResult, 0, len(cases))}
	for _, c := range cases {
		res := CaseResult{Image: c.Image, Instruction: c.Instruction}

		if data, ferr := os.ReadFile(filepath.Join(dir, c.Image)); ferr != nil {
			res.Err = fmt.Sprintf("bench: read image: %v", ferr)
		} else if pt, perr := p.PredictPoint(ctx, action.Image{MIME: action.MIMEPNG, Data: data}, c.Instruction); perr != nil {
			res.Err = perr.Error()
		} else {
			res.Point = pt
			res.Hit = c.Box.Contains(pt)
			if res.Hit {
				rep.Hits++
			}
		}

		rep.Results = append(rep.Results, res)
		rep.Total++
	}
	if rep.Total > 0 {
		rep.Accuracy = float64(rep.Hits) / float64(rep.Total)
	}
	return rep, nil
}
