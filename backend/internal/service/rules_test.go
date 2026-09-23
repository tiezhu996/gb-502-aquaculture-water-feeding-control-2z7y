package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"strings"
	"testing"
	"time"
)

func TestAssessWaterRisk(t *testing.T) {
	tests := []struct {
		name        string
		input       dto.WaterReadingInput
		wantRisk    constants.RiskLevel
		messagePart string
	}{
		{
			name: "all measurements normal",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 6.2,
				Temperature:     26,
				PH:              7.5,
				Ammonia:         0.1,
				Turbidity:       30,
			},
			wantRisk:    constants.RiskNormal,
			messagePart: "控制范围",
		},
		{
			name: "oxygen warning",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 4.2,
				Temperature:     26,
				PH:              7.5,
				Ammonia:         0.1,
				Turbidity:       30,
			},
			wantRisk:    constants.RiskWarning,
			messagePart: "溶解氧",
		},
		{
			name: "oxygen critical",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 2.8,
				Temperature:     26,
				PH:              7.5,
				Ammonia:         0.1,
				Turbidity:       30,
			},
			wantRisk:    constants.RiskCritical,
			messagePart: "严重偏低",
		},
		{
			name: "ph warning",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 6,
				Temperature:     26,
				PH:              6.2,
				Ammonia:         0.1,
				Turbidity:       30,
			},
			wantRisk:    constants.RiskWarning,
			messagePart: "pH",
		},
		{
			name: "ammonia critical",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 6,
				Temperature:     26,
				PH:              7.5,
				Ammonia:         1.2,
				Turbidity:       30,
			},
			wantRisk:    constants.RiskCritical,
			messagePart: "氨氮",
		},
		{
			name: "temperature warning",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 6,
				Temperature:     33,
				PH:              7.5,
				Ammonia:         0.1,
				Turbidity:       30,
			},
			wantRisk:    constants.RiskWarning,
			messagePart: "水温",
		},
		{
			name: "turbidity warning",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 6,
				Temperature:     26,
				PH:              7.5,
				Ammonia:         0.1,
				Turbidity:       120,
			},
			wantRisk:    constants.RiskWarning,
			messagePart: "浊度",
		},
		{
			name: "critical takes precedence over warnings",
			input: dto.WaterReadingInput{
				DissolvedOxygen: 2,
				Temperature:     33,
				PH:              7.5,
				Ammonia:         0.5,
				Turbidity:       120,
			},
			wantRisk:    constants.RiskCritical,
			messagePart: "浊度",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotRisk, gotMessage := assessWaterRisk(test.input)
			if gotRisk != test.wantRisk {
				t.Fatalf("risk = %q, want %q; message=%q", gotRisk, test.wantRisk, gotMessage)
			}
			if !strings.Contains(gotMessage, test.messagePart) {
				t.Fatalf("message %q does not contain %q", gotMessage, test.messagePart)
			}
		})
	}
}

func TestExecutionStatusCannotMoveBackToScheduled(t *testing.T) {
	if constants.ExecutionRunning.CanTransitionTo(constants.ExecutionScheduled) {
		t.Fatal("running execution must not return to scheduled")
	}
	if !constants.ExecutionScheduled.CanTransitionTo(constants.ExecutionRunning) {
		t.Fatal("scheduled execution should be able to start")
	}
	if constants.ExecutionCompleted.CanTransitionTo(constants.ExecutionRunning) {
		t.Fatal("completed execution must be terminal")
	}
}

func TestAbortTransitionsAndTerminality(t *testing.T) {
	if !constants.ExecutionScheduled.CanTransitionTo(constants.ExecutionAborted) {
		t.Fatal("scheduled execution should be abortable")
	}
	if !constants.ExecutionRunning.CanTransitionTo(constants.ExecutionAborted) {
		t.Fatal("running execution should be abortable")
	}
	if constants.ExecutionAborted.CanTransitionTo(constants.ExecutionRunning) ||
		constants.ExecutionAborted.CanTransitionTo(constants.ExecutionCompleted) ||
		constants.ExecutionAborted.CanTransitionTo(constants.ExecutionScheduled) {
		t.Fatal("aborted execution must be terminal")
	}
	if !constants.ExecutionAborted.Valid() {
		t.Fatal("aborted must be a valid status")
	}
}

func TestSameUTCDay(t *testing.T) {
	base := time.Date(2026, 9, 23, 23, 0, 0, 0, time.UTC)
	sameDay := base.Add(30 * time.Minute)
	nextDay := base.Add(2 * time.Hour)
	otherDay := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	if !sameUTCDay(base, sameDay) {
		t.Fatal("times within the same UTC day should match")
	}
	if sameUTCDay(base, nextDay) {
		t.Fatal("times crossing UTC midnight should not match")
	}
	if sameUTCDay(base, otherDay) {
		t.Fatal("different calendar days should not match")
	}
}
