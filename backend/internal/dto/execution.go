package dto

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

type ExecutionInput struct {
	PondID          uint      `json:"pondId" binding:"required"`
	FeedingPlanID   uint      `json:"feedingPlanId" binding:"required"`
	ScheduledAt     time.Time `json:"scheduledAt" binding:"required"`
	PlannedAmountKg float64   `json:"plannedAmountKg" binding:"required,gt=0"`
	Weather         string    `json:"weather" binding:"max=120"`
}

type UpdateExecutionInput struct {
	ScheduledAt     time.Time                 `json:"scheduledAt" binding:"required"`
	PlannedAmountKg float64                   `json:"plannedAmountKg" binding:"required,gt=0"`
	Weather         string                    `json:"weather" binding:"max=120"`
	Status          constants.ExecutionStatus `json:"status" binding:"required,oneof=scheduled running cancelled"`
}

type CompleteExecutionInput struct {
	ActualAmountKg float64 `json:"actualAmountKg" binding:"required,gt=0"`
	OxygenSnapshot float64 `json:"oxygenSnapshot" binding:"gte=0,lte=30"`
	Feedback       string  `json:"feedback" binding:"required,min=2,max=1000"`
}

type AbortExecutionInput struct {
	// ActualAmountKg 为中止前实际投喂量，允许为 0（完全未投喂）。
	ActualAmountKg float64 `json:"actualAmountKg" binding:"gte=0"`
	AbortReason    string  `json:"abortReason" binding:"required,min=2,max=1000"`
}

type RescheduleExecutionInput struct {
	// ScheduledAt 补排时间，必须与被中止记录安排在同一 UTC 日。
	ScheduledAt time.Time `json:"scheduledAt" binding:"required"`
	// PlannedAmountKg 补排量，只能使用当日剩余的日量差额。
	PlannedAmountKg float64 `json:"plannedAmountKg" binding:"required,gt=0"`
	Weather         string  `json:"weather" binding:"max=120"`
}
