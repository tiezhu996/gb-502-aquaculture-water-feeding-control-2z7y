package model

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

type ControlExecution struct {
	Base
	PondID        uint         `gorm:"not null;index" json:"pondId"`
	Pond          *Pond        `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`
	FeedingPlanID uint         `gorm:"not null;index" json:"feedingPlanId"`
	FeedingPlan   *FeedingPlan `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"feedingPlan,omitempty"`
	ScheduledAt   time.Time    `gorm:"not null;index" json:"scheduledAt"`
	StartedAt     *time.Time   `json:"startedAt"`
	CompletedAt   *time.Time   `json:"completedAt"`
	AbortedAt     *time.Time   `json:"abortedAt"`
	// PlannedAmountKg 是本次安排计划占用的日量；中止后不再按该值占用日累计。
	PlannedAmountKg float64 `gorm:"not null" json:"plannedAmountKg"`
	// ActualAmountKg 为实际投喂量；中止时按此值计入当日累计，可为 0。
	ActualAmountKg float64                   `gorm:"not null;default:0" json:"actualAmountKg"`
	Status         constants.ExecutionStatus `gorm:"size:20;not null;index" json:"status"`
	Operator       string                    `gorm:"size:80;not null" json:"operator"`
	Weather        string                    `gorm:"size:120" json:"weather"`
	OxygenSnapshot float64                   `json:"oxygenSnapshot"`
	Feedback       string                    `gorm:"type:text" json:"feedback"`
	AbortReason    string                    `gorm:"type:text" json:"abortReason"`
	// 补排关系：补排记录指向被补排的中止记录（RescheduledFromID），
	// 中止记录保留并指向生成的补排记录（RescheduledToID）。
	RescheduledFromID *uint `gorm:"index" json:"rescheduledFromId,omitempty"`
	RescheduledToID   *uint `gorm:"index" json:"rescheduledToId,omitempty"`
	// 关联记录的只读视图，由仓储层单向挂载，避免循环序列化。
	RescheduledFrom *ControlExecution `gorm:"foreignKey:RescheduledFromID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"rescheduledFrom,omitempty"`
	RescheduledTo   *ControlExecution `gorm:"foreignKey:RescheduledToID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"rescheduledTo,omitempty"`
}
