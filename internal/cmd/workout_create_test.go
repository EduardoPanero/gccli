package cmd

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/bpauli/gccli/internal/outfmt"
)

func TestParseCondition(t *testing.T) {
	tests := []struct {
		input    string
		wantType string
		want     float64
		wantErr  bool
	}{
		{"1min", "time", 60, false},
		{"5m", "distance", 5, false},
		{"400m", "distance", 400, false},
		{"1km", "distance", 1000, false},
		{"2mi", "distance", 3218.688, false},
		{"30s", "time", 30, false},
		{"1min30s", "time", 90, false},
		{"2min15s", "time", 135, false},
		{"", "", 0, true},
		{"abc", "", 0, true},
		{"0min", "", 0, true},
		{"0m", "", 0, true},
		{"0s", "", 0, true},
		{"0m0s", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotType, gotVal, err := parseCondition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseCondition(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if gotType != tt.wantType {
					t.Errorf("parseCondition(%q) type = %v, want %v", tt.input, gotType, tt.wantType)
				}
				if gotVal != tt.want {
					t.Errorf("parseCondition(%q) val = %v, want %v", tt.input, gotVal, tt.want)
				}
			}
		})
	}
}

func TestParsePaceToMPS(t *testing.T) {
	tests := []struct {
		input   string
		unit    string
		want    float64
		wantErr bool
	}{
		{"5:30", "km", 1000.0 / 330.0, false},
		{"6:00", "km", 1000.0 / 360.0, false},
		{"5:00", "km", 1000.0 / 300.0, false},
		{"5:15", "km", 1000.0 / 315.0, false},
		{"8:51", "mi", 1609.344 / 531.0, false},
		{"9:39", "mi", 1609.344 / 579.0, false},
		{"0:00", "km", 0, true},
		{"bad", "km", 0, true},
		{"5:3", "km", 0, true}, // must be M:SS
		{"", "km", 0, true},
	}

	for _, tt := range tests {
		name := tt.input + "_" + tt.unit
		t.Run(name, func(t *testing.T) {
			got, err := parsePaceToMPS(tt.input, tt.unit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePaceToMPS(%q, %q) error = %v, wantErr %v", tt.input, tt.unit, err, tt.wantErr)
			}
			if !tt.wantErr && math.Abs(got-tt.want) > 0.0001 {
				t.Errorf("parsePaceToMPS(%q, %q) = %v, want %v", tt.input, tt.unit, got, tt.want)
			}
		})
	}
}

func TestParseNumericRange(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLow  float64
		wantHigh float64
		wantErr  bool
	}{
		{"heart rate", "140-160", 140, 160, false},
		{"power", "250-280", 250, 280, false},
		{"cadence", "170-180", 170, 180, false},
		{"missing separator", "140160", 0, 0, true},
		{"equal values", "150-150", 0, 0, true},
		{"reversed", "160-140", 0, 0, true},
		{"bad low", "abc-160", 0, 0, true},
		{"bad high", "140-abc", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			low, high, err := parseNumericRange(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNumericRange(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if low != tt.wantLow {
					t.Errorf("low = %v, want %v", low, tt.wantLow)
				}
				if high != tt.wantHigh {
					t.Errorf("high = %v, want %v", high, tt.wantHigh)
				}
			}
		})
	}
}

func TestParseStep(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		unit    string
		wantErr bool
		check   func(t *testing.T, s workoutStep)
	}{
		{
			name:  "warmup with pace target",
			input: "warmup:1min@pace:5:30-6:00",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "warmup" {
					t.Errorf("stepType = %q, want warmup", s.stepType)
				}
				if s.endConditionType != "time" {
					t.Errorf("endConditionType = %q, want time", s.endConditionType)
				}
				if s.endConditionValue != 60 {
					t.Errorf("endConditionValue = %v, want 60", s.endConditionValue)
				}
				if s.targetType != "pace" {
					t.Errorf("targetType = %q, want pace", s.targetType)
				}
				wantFast := 1000.0 / 330.0
				if math.Abs(s.targetValueOne-wantFast) > 0.001 {
					t.Errorf("targetValueOne = %v, want ~%v", s.targetValueOne, wantFast)
				}
				wantSlow := 1000.0 / 360.0
				if math.Abs(s.targetValueTwo-wantSlow) > 0.001 {
					t.Errorf("targetValueTwo = %v, want ~%v", s.targetValueTwo, wantSlow)
				}
			},
		},
		{
			name:  "run without target",
			input: "run:5min",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "run" {
					t.Errorf("stepType = %q, want run", s.stepType)
				}
				if s.endConditionType != "time" {
					t.Errorf("endConditionType = %q, want time", s.endConditionType)
				}
				if s.endConditionValue != 300 {
					t.Errorf("endConditionValue = %v, want 300", s.endConditionValue)
				}
				if s.targetType != "" {
					t.Errorf("targetType = %q, want empty", s.targetType)
				}
			},
		},
		{
			name:  "run with distance",
			input: "run:400m",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "run" {
					t.Errorf("stepType = %q, want run", s.stepType)
				}
				if s.endConditionType != "distance" {
					t.Errorf("endConditionType = %q, want distance", s.endConditionType)
				}
				if s.endConditionValue != 400 {
					t.Errorf("endConditionValue = %v, want 400", s.endConditionValue)
				}
			},
		},
		{
			name:  "cooldown with seconds duration and pace",
			input: "cooldown:90s@pace:5:00-5:30",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "cooldown" {
					t.Errorf("stepType = %q, want cooldown", s.stepType)
				}
				if s.endConditionType != "time" {
					t.Errorf("endConditionType = %q, want time", s.endConditionType)
				}
				if s.endConditionValue != 90 {
					t.Errorf("endConditionValue = %v, want 90", s.endConditionValue)
				}
				if s.targetType != "pace" {
					t.Errorf("targetType = %q, want pace", s.targetType)
				}
			},
		},
		{
			name:  "recovery with combined duration",
			input: "recovery:1min30s@pace:5:00-5:30",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "recovery" {
					t.Errorf("stepType = %q, want recovery", s.stepType)
				}
				if s.endConditionType != "time" {
					t.Errorf("endConditionType = %q, want time", s.endConditionType)
				}
				if s.endConditionValue != 90 {
					t.Errorf("endConditionValue = %v, want 90", s.endConditionValue)
				}
			},
		},
		{
			name:  "rest without target",
			input: "rest:2min",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "rest" {
					t.Errorf("stepType = %q, want rest", s.stepType)
				}
				if s.endConditionType != "time" {
					t.Errorf("endConditionType = %q, want time", s.endConditionType)
				}
				if s.endConditionValue != 120 {
					t.Errorf("endConditionValue = %v, want 120", s.endConditionValue)
				}
			},
		},
		{
			name:  "imperial pace",
			input: "run:5min@pace:8:51-9:39",
			unit:  "mi",
			check: func(t *testing.T, s workoutStep) {
				if s.targetType != "pace" {
					t.Fatalf("targetType = %q, want pace", s.targetType)
				}
				wantFast := 1609.344 / 531.0
				if math.Abs(s.targetValueOne-wantFast) > 0.001 {
					t.Errorf("targetValueOne = %v, want ~%v", s.targetValueOne, wantFast)
				}
			},
		},
		{
			name:  "heart rate target",
			input: "run:20min@hr:140-160",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.targetType != "hr" {
					t.Errorf("targetType = %q, want hr", s.targetType)
				}
				if s.targetValueOne != 140 {
					t.Errorf("targetValueOne = %v, want 140", s.targetValueOne)
				}
				if s.targetValueTwo != 160 {
					t.Errorf("targetValueTwo = %v, want 160", s.targetValueTwo)
				}
			},
		},
		{
			name:  "power target",
			input: "run:5min@power:250-280",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.targetType != "power" {
					t.Errorf("targetType = %q, want power", s.targetType)
				}
				if s.targetValueOne != 250 {
					t.Errorf("targetValueOne = %v, want 250", s.targetValueOne)
				}
				if s.targetValueTwo != 280 {
					t.Errorf("targetValueTwo = %v, want 280", s.targetValueTwo)
				}
			},
		},
		{
			name:  "cadence target",
			input: "run:10min@cadence:170-180",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.targetType != "cadence" {
					t.Errorf("targetType = %q, want cadence", s.targetType)
				}
				if s.targetValueOne != 170 {
					t.Errorf("targetValueOne = %v, want 170", s.targetValueOne)
				}
				if s.targetValueTwo != 180 {
					t.Errorf("targetValueTwo = %v, want 180", s.targetValueTwo)
				}
			},
		},
		{
			name:  "other step type",
			input: "other:3min",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "other" {
					t.Errorf("stepType = %q, want other", s.stepType)
				}
				if s.endConditionType != "time" {
					t.Errorf("endConditionType = %q, want time", s.endConditionType)
				}
				if s.endConditionValue != 180 {
					t.Errorf("endConditionValue = %v, want 180", s.endConditionValue)
				}
			},
		},
		{
			name:  "interval step type",
			input: "interval:5min@hr:150-170",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "interval" {
					t.Errorf("stepType = %q, want interval", s.stepType)
				}
				if s.targetType != "hr" {
					t.Errorf("targetType = %q, want hr", s.targetType)
				}
			},
		},
		{
			name:  "repeat step type",
			input: "repeat:3:run:10min@hr:150-170+rest:1min",
			unit:  "km",
			check: func(t *testing.T, s workoutStep) {
				if s.stepType != "repeat" {
					t.Errorf("stepType = %q, want repeat", s.stepType)
				}
				if s.targetType != "hr" {
					t.Errorf("targetType = %q, want hr", s.targetType)
				}
			},
		},
		{
			name:    "invalid type",
			input:   "sprint:1min",
			unit:    "km",
			wantErr: true,
		},
		{
			name:    "missing duration",
			input:   "warmup",
			unit:    "km",
			wantErr: true,
		},
		{
			name:    "invalid pace order",
			input:   "run:1min@pace:6:00-5:30",
			unit:    "km",
			wantErr: true,
		},
		{
			name:    "invalid target type",
			input:   "run:5min@speed:10-12",
			unit:    "km",
			wantErr: true,
		},
		{
			name:    "target without values",
			input:   "run:5min@hr",
			unit:    "km",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStep(tt.input, tt.unit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseStep(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.check != nil && err == nil {
				tt.check(t, got)
			}
		})
	}
}

func TestBuildWorkoutJSON(t *testing.T) {
	steps := []workoutStep{
		{stepType: "warmup", endConditionType: "time", endConditionValue: 300, targetType: "pace", targetValueOne: 3.030303, targetValueTwo: 2.777778},
		{stepType: "run", endConditionType: "time", endConditionValue: 1200, targetType: "hr", targetValueOne: 140, targetValueTwo: 160},
		{stepType: "cooldown", endConditionType: "time", endConditionValue: 300},
	}

	data, err := buildWorkoutJSON("Test Workout", "run", steps)
	if err != nil {
		t.Fatalf("buildWorkoutJSON: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if payload["workoutName"] != "Test Workout" {
		t.Errorf("workoutName = %v, want Test Workout", payload["workoutName"])
	}

	sportType, ok := payload["sportType"].(map[string]any)
	if !ok {
		t.Fatal("sportType not a map")
	}
	if sportType["sportTypeId"] != float64(1) {
		t.Errorf("sportTypeId = %v, want 1", sportType["sportTypeId"])
	}
	if sportType["sportTypeKey"] != "running" {
		t.Errorf("sportTypeKey = %v, want running", sportType["sportTypeKey"])
	}

	segments, ok := payload["workoutSegments"].([]any)
	if !ok || len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %v", payload["workoutSegments"])
	}

	seg := segments[0].(map[string]any)
	segSport := seg["sportType"].(map[string]any)
	if segSport["sportTypeId"] != float64(1) {
		t.Errorf("segment sportTypeId = %v, want 1", segSport["sportTypeId"])
	}

	workoutSteps, ok := seg["workoutSteps"].([]any)
	if !ok || len(workoutSteps) != 3 {
		t.Fatalf("expected 3 workout steps, got %d", len(workoutSteps))
	}

	// Step 1: warmup with pace target.
	s1 := workoutSteps[0].(map[string]any)
	if s1["type"] != "ExecutableStepDTO" {
		t.Errorf("step 1 type = %v", s1["type"])
	}
	if s1["stepOrder"] != float64(1) {
		t.Errorf("step 1 stepOrder = %v", s1["stepOrder"])
	}
	st1 := s1["stepType"].(map[string]any)
	if st1["stepTypeId"] != float64(1) || st1["stepTypeKey"] != "warmup" {
		t.Errorf("step 1 stepType = %v", st1)
	}
	if s1["endConditionValue"] != float64(300) {
		t.Errorf("step 1 endConditionValue = %v, want 300", s1["endConditionValue"])
	}
	tt1 := s1["targetType"].(map[string]any)
	if tt1["workoutTargetTypeId"] != float64(6) || tt1["workoutTargetTypeKey"] != "pace.zone" {
		t.Errorf("step 1 targetType = %v", tt1)
	}

	// Step 2: run (interval) with heart rate target.
	s2 := workoutSteps[1].(map[string]any)
	st2 := s2["stepType"].(map[string]any)
	if st2["stepTypeId"] != float64(3) || st2["stepTypeKey"] != "interval" {
		t.Errorf("step 2 stepType = %v", st2)
	}
	if s2["endConditionValue"] != float64(1200) {
		t.Errorf("step 2 endConditionValue = %v, want 1200", s2["endConditionValue"])
	}
	tt2 := s2["targetType"].(map[string]any)
	if tt2["workoutTargetTypeId"] != float64(4) || tt2["workoutTargetTypeKey"] != "heart.rate.zone" {
		t.Errorf("step 2 targetType = %v", tt2)
	}
	if s2["targetValueOne"] != float64(140) {
		t.Errorf("step 2 targetValueOne = %v, want 140", s2["targetValueOne"])
	}
	if s2["targetValueTwo"] != float64(160) {
		t.Errorf("step 2 targetValueTwo = %v, want 160", s2["targetValueTwo"])
	}

	// Step 3: cooldown with no target.
	s3 := workoutSteps[2].(map[string]any)
	st3 := s3["stepType"].(map[string]any)
	if st3["stepTypeId"] != float64(2) || st3["stepTypeKey"] != "cooldown" {
		t.Errorf("step 3 stepType = %v", st3)
	}
	tt3 := s3["targetType"].(map[string]any)
	if tt3["workoutTargetTypeId"] != float64(1) || tt3["workoutTargetTypeKey"] != "no.target" {
		t.Errorf("step 3 targetType = %v, want no.target", tt3)
	}
}

func TestBuildWorkoutJSON_CyclingSport(t *testing.T) {
	steps := []workoutStep{
		{stepType: "warmup", endConditionType: "time", endConditionValue: 600},
		{stepType: "run", endConditionType: "time", endConditionValue: 300, targetType: "power", targetValueOne: 250, targetValueTwo: 280},
		{stepType: "cooldown", endConditionType: "time", endConditionValue: 600},
	}

	data, err := buildWorkoutJSON("FTP Intervals", "bike", steps)
	if err != nil {
		t.Fatalf("buildWorkoutJSON: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	sportType := payload["sportType"].(map[string]any)
	if sportType["sportTypeId"] != float64(2) {
		t.Errorf("sportTypeId = %v, want 2", sportType["sportTypeId"])
	}
	if sportType["sportTypeKey"] != "cycling" {
		t.Errorf("sportTypeKey = %v, want cycling", sportType["sportTypeKey"])
	}

	segments := payload["workoutSegments"].([]any)
	seg := segments[0].(map[string]any)
	segSport := seg["sportType"].(map[string]any)
	if segSport["sportTypeId"] != float64(2) {
		t.Errorf("segment sportTypeId = %v, want 2", segSport["sportTypeId"])
	}

	workoutSteps := seg["workoutSteps"].([]any)
	s2 := workoutSteps[1].(map[string]any)
	tt2 := s2["targetType"].(map[string]any)
	if tt2["workoutTargetTypeId"] != float64(2) || tt2["workoutTargetTypeKey"] != "power.zone" {
		t.Errorf("step 2 targetType = %v, want power.zone", tt2)
	}
}

func TestBuildWorkoutJSON_UnknownSport(t *testing.T) {
	_, err := buildWorkoutJSON("Bad", "unknown", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWorkoutCreateCmd_Run_JSON(t *testing.T) {
	server := workoutsTestServer(t)
	defer server.Close()

	store := newTestSecretsStore(t)
	overrideLoadSecrets(t, store)
	overrideNewClient(t, server)
	storeTestTokens(t, store, "test@example.com", testTokens())

	var buf bytes.Buffer
	g := testGlobals(t, &buf, outfmt.JSON, "test@example.com")
	cmd := &WorkoutCreateCmd{
		Name:  "Test Run",
		Type:  "run",
		Steps: []string{"warmup:1min@pace:5:30-6:00", "run:5min@pace:5:15-5:30", "cooldown:1min@pace:5:30-6:00"},
		Unit:  "km",
	}

	err := cmd.Run(g)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestWorkoutCreateCmd_Run_Table(t *testing.T) {
	server := workoutsTestServer(t)
	defer server.Close()

	store := newTestSecretsStore(t)
	overrideLoadSecrets(t, store)
	overrideNewClient(t, server)
	storeTestTokens(t, store, "test@example.com", testTokens())

	var buf bytes.Buffer
	g := testGlobals(t, &buf, outfmt.Table, "test@example.com")
	cmd := &WorkoutCreateCmd{
		Name:  "Test Run",
		Type:  "run",
		Steps: []string{"warmup:1min", "run:5min@pace:5:15-5:30", "cooldown:1min"},
		Unit:  "km",
	}

	err := cmd.Run(g)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output, got empty buffer")
	}
}

func TestWorkoutCreateCmd_Run_NoAccount(t *testing.T) {
	var buf bytes.Buffer
	g := testGlobals(t, &buf, outfmt.Table, "")
	cmd := &WorkoutCreateCmd{
		Name:  "Test Run",
		Type:  "run",
		Steps: []string{"warmup:1min"},
		Unit:  "km",
	}

	err := cmd.Run(g)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWorkoutCreateCmd_Run_InvalidStep(t *testing.T) {
	var buf bytes.Buffer
	g := testGlobals(t, &buf, outfmt.Table, "test@example.com")
	cmd := &WorkoutCreateCmd{
		Name:  "Test Run",
		Type:  "run",
		Steps: []string{"invalid"},
		Unit:  "km",
	}

	err := cmd.Run(g)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecute_WorkoutsCreateHelp(t *testing.T) {
	code := Execute([]string{"workouts", "create", "--help"}, "1.0.0", "abc123", "2024-01-01")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}
