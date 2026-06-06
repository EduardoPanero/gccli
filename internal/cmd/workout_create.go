package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/bpauli/gccli/internal/outfmt"
)

const (
	metersPerKm   = 1000.0
	metersPerMile = 1609.344
)

// WorkoutCreateCmd creates a workout with configurable sport type and targets.
type WorkoutCreateCmd struct {
	Name  string   `arg:"" help:"Workout name."`
	Type  string   `help:"Sport type." required:"" enum:"run,bike,swim,strength,cardio,hiit,yoga,pilates,mobility,multisport,custom"`
	Steps []string `help:"Workout step (type:condition[@target:values] or repeat:iters:step1+step2). Condition = time (5min, 1min30s) or distance (400m, 1km, 1.5mi)." required:"" short:"s" name:"step"`
	Unit  string   `help:"Pace unit: km or mi." default:"km" enum:"km,mi"`
}

type workoutStep struct {
	isRepeat   bool
	iterations int
	steps      []workoutStep

	stepType          string
	endConditionType  string // "time" or "distance"
	endConditionValue float64 // seconds or meters
	targetType        string  // "pace", "hr", "power", "cadence", or "" for no target
	targetValueOne    float64 // higher/faster value
	targetValueTwo    float64 // lower/slower value
}

var stepTypeMap = map[string]struct {
	id  int
	key string
}{
	"warmup":   {1, "warmup"},
	"cooldown": {2, "cooldown"},
	"run":      {3, "interval"},
	"interval": {3, "interval"},
	"recovery": {4, "recovery"},
	"rest":     {5, "rest"},
	"other":    {7, "other"},
}

var sportTypeMap = map[string]struct {
	id  int
	key string
}{
	"run":        {1, "running"},
	"bike":       {2, "cycling"},
	"custom":     {3, "other"},
	"swim":       {4, "swimming"},
	"strength":   {5, "strength_training"},
	"cardio":     {6, "cardio_training"},
	"yoga":       {7, "yoga"},
	"pilates":    {8, "pilates"},
	"hiit":       {9, "hiit"},
	"multisport": {10, "multi_sport"},
	"mobility":   {11, "mobility"},
}

var targetTypeMap = map[string]struct {
	id  int
	key string
}{
	"pace":    {6, "pace.zone"},
	"hr":      {4, "heart.rate.zone"},
	"power":   {2, "power.zone"},
	"cadence": {3, "cadence"},
}

// mixedTimeRegexp matches mixed time like "1min30s" or "30seg".
var mixedTimeRegexp = regexp.MustCompile(`^(?:(\d+)min)?(?:(\d+)(?:s|seg))$`)

// singleUnitRegexp matches single units like "1km", "400m", "5mi", "5min".
var singleUnitRegexp = regexp.MustCompile(`^([0-9.]+)(km|mi|m|mt|mts|min)$`)

// paceRegexp matches pace like "5:30".
var paceRegexp = regexp.MustCompile(`^(\d+):(\d{2})$`)

func (c *WorkoutCreateCmd) Run(g *Globals) error {
	client, err := resolveClient(g)
	if err != nil {
		return err
	}

	steps, err := parseSteps(c.Steps, c.Unit)
	if err != nil {
		return err
	}

	payload, err := buildWorkoutJSON(c.Name, c.Type, steps)
	if err != nil {
		return fmt.Errorf("build workout: %w", err)
	}

	data, err := client.UploadWorkout(g.Context, payload)
	if err != nil {
		return fmt.Errorf("create workout: %w", err)
	}

	if outfmt.IsJSON(g.Context) {
		return outfmt.WriteJSON(os.Stdout, json.RawMessage(data))
	}

	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err == nil {
		if id := jsonString(resp, "workoutId"); id != "" {
			g.UI.Successf("Created workout %q (ID: %s)", c.Name, id)
			return nil
		}
	}

	g.UI.Successf("Created workout %q", c.Name)
	return nil
}

func parseSteps(raw []string, unit string) ([]workoutStep, error) {
	steps := make([]workoutStep, 0, len(raw))
	for _, s := range raw {
		step, err := parseStep(s, unit)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func parseStep(s string, unit string) (workoutStep, error) {
	if strings.HasPrefix(s, "repeat:") {
		parts := strings.SplitN(s, ":", 3)
		if len(parts) != 3 {
			return workoutStep{}, fmt.Errorf("invalid repeat step %q: expected repeat:iterations:step1+step2", s)
		}
		iters, err := strconv.Atoi(parts[1])
		if err != nil || iters <= 0 {
			return workoutStep{}, fmt.Errorf("invalid repeat iterations %q", parts[1])
		}

		innerStepStrs := strings.Split(parts[2], "+")
		var innerSteps []workoutStep
		for _, is := range innerStepStrs {
			inner, err := parseStep(is, unit)
			if err != nil {
				return workoutStep{}, fmt.Errorf("in repeat: %w", err)
			}
			if inner.isRepeat {
				return workoutStep{}, fmt.Errorf("nested repeats are not supported")
			}
			innerSteps = append(innerSteps, inner)
		}

		if len(innerSteps) == 0 {
			return workoutStep{}, fmt.Errorf("repeat must have at least one step")
		}

		return workoutStep{
			isRepeat:   true,
			iterations: iters,
			steps:      innerSteps,
		}, nil
	}

	// Split on first "@" to separate step part from optional target part.
	stepPart, targetPart, _ := strings.Cut(s, "@")

	// Parse step part: "type:duration"
	typeName, durStr, found := strings.Cut(stepPart, ":")
	if !found {
		return workoutStep{}, fmt.Errorf("invalid step %q: expected type:duration[@target:values]", s)
	}

	if _, ok := stepTypeMap[typeName]; !ok {
		return workoutStep{}, fmt.Errorf("invalid step type %q: expected warmup, run, interval, cooldown, recovery, rest, or other", typeName)
	}

	condType, condVal, err := parseCondition(durStr)
	if err != nil {
		return workoutStep{}, fmt.Errorf("invalid step %q: %w", s, err)
	}

	step := workoutStep{
		stepType:          typeName,
		endConditionType:  condType,
		endConditionValue: condVal,
	}

	if targetPart != "" {
		targetName, values, found := strings.Cut(targetPart, ":")
		if !found {
			return workoutStep{}, fmt.Errorf("invalid step %q: target requires values after ':'", s)
		}

		if _, ok := targetTypeMap[targetName]; !ok {
			return workoutStep{}, fmt.Errorf("invalid target type %q: expected pace, hr, power, or cadence", targetName)
		}

		step.targetType = targetName

		switch targetName {
		case "pace":
			high, low, err := parsePaceRange(values, unit)
			if err != nil {
				return workoutStep{}, fmt.Errorf("invalid step %q: %w", s, err)
			}
			step.targetValueOne = high
			step.targetValueTwo = low
		default:
			low, high, err := parseNumericRange(values)
			if err != nil {
				return workoutStep{}, fmt.Errorf("invalid step %q: %w", s, err)
			}
			step.targetValueOne = low
			step.targetValueTwo = high
		}
	}

	return step, nil
}

func parseCondition(s string) (string, float64, error) {
	if s == "" {
		return "", 0, fmt.Errorf("empty condition")
	}

	// Check if it's mixed time like 1m30s or 30s
	if m := mixedTimeRegexp.FindStringSubmatch(s); m != nil {
		var total float64
		if m[1] != "" {
			mins, _ := strconv.ParseFloat(m[1], 64)
			total += mins * 60
		}
		if m[2] != "" {
			secs, _ := strconv.ParseFloat(m[2], 64)
			total += secs
		}
		if total == 0 {
			return "", 0, fmt.Errorf("time must be greater than zero")
		}
		return "time", total, nil
	}

	// Check single unit
	if m := singleUnitRegexp.FindStringSubmatch(s); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return "", 0, fmt.Errorf("invalid value %q", m[1])
		}
		if v <= 0 {
			return "", 0, fmt.Errorf("value must be greater than zero")
		}

		switch m[2] {
		case "km":
			return "distance", v * 1000.0, nil
		case "mi":
			return "distance", v * 1609.344, nil
		case "m", "mt", "mts":
			return "distance", v, nil
		case "min":
			return "time", v * 60.0, nil
		}
	}

	return "", 0, fmt.Errorf("invalid condition %q: expected format like 1km, 400m, 5mi, 5min, 1m30s", s)
}

// parseNumericRange parses "140-160" into (140.0, 160.0).
func parseNumericRange(s string) (float64, float64, error) {
	low, high, found := strings.Cut(s, "-")
	if !found {
		return 0, 0, fmt.Errorf("invalid range %q: expected LOW-HIGH", s)
	}

	lowVal, err := strconv.ParseFloat(low, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range low value %q: %w", low, err)
	}

	highVal, err := strconv.ParseFloat(high, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range high value %q: %w", high, err)
	}

	if lowVal >= highVal {
		return 0, 0, fmt.Errorf("range low value %.0f must be less than high value %.0f", lowVal, highVal)
	}

	return lowVal, highVal, nil
}

// parsePaceRange parses "5:30-6:00" into (fastMPS, slowMPS).
func parsePaceRange(s string, unit string) (float64, float64, error) {
	idx := findPaceSeparator(s)
	if idx < 0 {
		return 0, 0, fmt.Errorf("invalid pace range %q: expected format like 5:30-6:00", s)
	}

	fastStr := s[:idx]
	slowStr := s[idx+1:]

	fastMPS, err := parsePaceToMPS(fastStr, unit)
	if err != nil {
		return 0, 0, err
	}

	slowMPS, err := parsePaceToMPS(slowStr, unit)
	if err != nil {
		return 0, 0, err
	}

	if fastMPS < slowMPS {
		return 0, 0, fmt.Errorf("fast pace %s must be faster (lower) than slow pace %s", fastStr, slowStr)
	}

	return fastMPS, slowMPS, nil
}

// findPaceSeparator finds the hyphen index in a pace range like "5:30-6:00".
// Returns -1 if not found.
func findPaceSeparator(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			left := s[:i]
			right := s[i+1:]
			if paceRegexp.MatchString(left) && paceRegexp.MatchString(right) {
				return i
			}
		}
	}
	return -1
}

func parsePaceToMPS(s string, unit string) (float64, error) {
	m := paceRegexp.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid pace %q: expected format like 5:30", s)
	}

	mins, _ := strconv.ParseFloat(m[1], 64)
	secs, _ := strconv.ParseFloat(m[2], 64)
	totalSecs := mins*60 + secs

	if totalSecs == 0 {
		return 0, fmt.Errorf("pace must be greater than zero")
	}

	dist := metersPerKm
	if unit == "mi" {
		dist = metersPerMile
	}

	return dist / totalSecs, nil
}

func buildWorkoutJSON(name string, sportType string, steps []workoutStep) (json.RawMessage, error) {
	sport, ok := sportTypeMap[sportType]
	if !ok {
		return nil, fmt.Errorf("unknown sport type: %s", sportType)
	}

	sportObj := map[string]any{
		"sportTypeId":  sport.id,
		"sportTypeKey": sport.key,
	}

	workoutSteps := make([]map[string]any, 0, len(steps))

	for i, s := range steps {
		if s.isRepeat {
			repeatSteps := make([]map[string]any, 0, len(s.steps))
			for j, rs := range s.steps {
				rsObj, err := buildExecutableStep(rs, j+1)
				if err != nil {
					return nil, err
				}
				repeatSteps = append(repeatSteps, rsObj)
			}

			stepObj := map[string]any{
				"type":               "RepeatGroupDTO",
				"stepOrder":          i + 1,
				"numberOfIterations": s.iterations,
				"workoutSteps":       repeatSteps,
				"smartRepeat":        false,
			}
			workoutSteps = append(workoutSteps, stepObj)
		} else {
			stepObj, err := buildExecutableStep(s, i+1)
			if err != nil {
				return nil, err
			}
			workoutSteps = append(workoutSteps, stepObj)
		}
	}

	payload := map[string]any{
		"workoutName": name,
		"sportType":   sportObj,
		"workoutSegments": []map[string]any{
			{
				"segmentOrder": 1,
				"sportType":    sportObj,
				"workoutSteps": workoutSteps,
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(data), nil
}

func buildExecutableStep(s workoutStep, order int) (map[string]any, error) {
	st, ok := stepTypeMap[s.stepType]
	if !ok {
		return nil, fmt.Errorf("unknown step type: %s", s.stepType)
	}

	step := map[string]any{
		"type":      "ExecutableStepDTO",
		"stepOrder": order,
		"stepType": map[string]any{
			"stepTypeId":  st.id,
			"stepTypeKey": st.key,
		},
		"endConditionValue": s.endConditionValue,
	}

	if s.endConditionType == "time" {
		step["endCondition"] = map[string]any{
			"conditionTypeId":  2,
			"conditionTypeKey": "time",
		}
	} else if s.endConditionType == "distance" {
		step["endCondition"] = map[string]any{
			"conditionTypeId":  3,
			"conditionTypeKey": "distance",
		}
	}

	if s.targetType != "" {
		tt := targetTypeMap[s.targetType]
		step["targetType"] = map[string]any{
			"workoutTargetTypeId":  tt.id,
			"workoutTargetTypeKey": tt.key,
		}
		step["targetValueOne"] = s.targetValueOne
		step["targetValueTwo"] = s.targetValueTwo
	} else {
		step["targetType"] = map[string]any{
			"workoutTargetTypeId":  1,
			"workoutTargetTypeKey": "no.target",
		}
	}

	return step, nil
}
