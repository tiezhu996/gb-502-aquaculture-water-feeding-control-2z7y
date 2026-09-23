package model

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

type ControlExecution struct {
	Base
	PondID          uint                      `gorm:"not null;index" json:"pondId"`
	Pond            *Pond                     `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`
	FeedingPlanID   uint                      `gorm:"not null;index" json:"feedingPlanId"`
	FeedingPlan     *FeedingPlan              `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"feedingPlan,omitempty"`
	ScheduledAt     time.Time                 `gorm:"not null;index" json:"scheduledAt"`
	StartedAt       *time.Time                `json:"startedAt"`
	CompletedAt     *time.Time                `json:"completedAt"`
	PlannedAmountKg float64                   `gorm:"not null" json:"plannedAmountKg"`
	ActualAmountKg  float64                   `gorm:"not null;default:0" json:"actualAmountKg"`
	Status          constants.ExecutionStatus `gorm:"size:20;not null;index" json:"status"`
	Operator        string                    `gorm:"size:80;not null" json:"operator"`
	Weather         string                    `gorm:"size:120" json:"weather"`
	OxygenSnapshot  float64                   `json:"oxygenSnapshot"`
	Feedback        string                    `gorm:"type:text" json:"feedback"`
	AbortReason     string                    `gorm:"type:text" json:"abortReason"`
	AbortedAt       *time.Time                `json:"abortedAt"`
	// RescheduleOfID 指向触发本次补排的已中止记录；原记录删除补排时置空。
	RescheduleOfID *uint             `gorm:"index" json:"rescheduleOfId,omitempty"`
	RescheduleOf   *ControlExecution `gorm:"foreignKey:RescheduleOfID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"rescheduleOf,omitempty"`
	// RescheduledTo 为反向补排关系，不单独落库列，按 RescheduleOfID 手工装载。
	RescheduledTo *ControlExecution `gorm:"-" json:"rescheduledTo,omitempty"`
}
