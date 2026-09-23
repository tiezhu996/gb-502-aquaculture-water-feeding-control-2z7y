package repository

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"

	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ExecutionRepository struct {
	db *gorm.DB
}

func NewExecutionRepository(db *gorm.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

func (r *ExecutionRepository) List(query dto.PageQuery, pondID, planID uint) ([]model.ControlExecution, int64, error) {
	base := r.db.Model(&model.ControlExecution{})
	if pondID > 0 {
		base = base.Where("pond_id = ?", pondID)
	}
	if planID > 0 {
		base = base.Where("feeding_plan_id = ?", planID)
	}
	if query.Status != "" {
		base = base.Where("status = ?", query.Status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var executions []model.ControlExecution
	err := base.Preload("Pond").Preload("FeedingPlan").
		Preload("RescheduleOf").Preload("RescheduleOf.Pond").Preload("RescheduleOf.FeedingPlan").
		Order("scheduled_at DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&executions).Error
	if err != nil {
		return nil, 0, err
	}
	if err := r.hydrateRescheduledTo(executions); err != nil {
		return nil, 0, err
	}
	return executions, total, nil
}

// hydrateRescheduledTo 依据本页记录的 reschedule_of_id 反向关系，批量装载“原中止记录 -> 补排记录”指针。
func (r *ExecutionRepository) hydrateRescheduledTo(executions []model.ControlExecution) error {
	idToIndex := make(map[uint]int, len(executions))
	var childIDs []uint
	for i := range executions {
		idToIndex[executions[i].ID] = i
		if executions[i].RescheduleOfID != nil {
			childIDs = append(childIDs, executions[i].ID)
		}
	}
	if len(childIDs) == 0 {
		return nil
	}
	var children []model.ControlExecution
	if err := r.db.Preload("Pond").Preload("FeedingPlan").
		Preload("RescheduleOf").Where("reschedule_of_id IN ?", childIDs).Find(&children).Error; err != nil {
		return err
	}
	for i := range children {
		child := children[i]
		if idx, ok := idToIndex[*child.RescheduleOfID]; ok {
			executions[idx].RescheduledTo = &child
		}
	}
	return nil
}

func (r *ExecutionRepository) Get(id uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	err := r.db.Preload("Pond").Preload("FeedingPlan").
		Preload("RescheduleOf").Preload("RescheduleOf.Pond").Preload("RescheduleOf.FeedingPlan").First(&execution, id).Error
	if err != nil {
		return execution, err
	}
	var child model.ControlExecution
	if err := r.db.Preload("Pond").Preload("FeedingPlan").
		Where("reschedule_of_id = ?", id).First(&child).Error; err == nil {
		execution.RescheduledTo = &child
	} else if err != gorm.ErrRecordNotFound {
		return execution, err
	}
	return execution, nil
}

func (r *ExecutionRepository) GetForUpdate(id uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Pond").Preload("FeedingPlan").
		Preload("RescheduleOf").Preload("RescheduleOf.Pond").Preload("RescheduleOf.FeedingPlan").First(&execution, id).Error
	if err != nil {
		return execution, err
	}
	var child model.ControlExecution
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Pond").Preload("FeedingPlan").
		Where("reschedule_of_id = ?", id).First(&child).Error; err == nil {
		execution.RescheduledTo = &child
	} else if err != gorm.ErrRecordNotFound {
		return execution, err
	}
	return execution, nil
}

func (r *ExecutionRepository) CountOpenForPlanExcluding(planID, excludedID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.ControlExecution{}).
		Where("feeding_plan_id = ? AND id <> ? AND status IN ?", planID, excludedID, []string{"scheduled", "running"}).Count(&count).Error
	return count, err
}

// OccupiedAmountForDay 核算某日（UTC）已占用/已投喂的额度：
// 中止记录按实际量计入（未投喂部分释放），其余未取消记录按计划量计入，取消记录不占用。
func (r *ExecutionRepository) OccupiedAmountForDay(pondID uint, from, until time.Time, excludedID uint) (float64, error) {
	var total float64
	err := r.db.Model(&model.ControlExecution{}).
		Where("pond_id = ? AND id <> ? AND scheduled_at >= ? AND scheduled_at < ?", pondID, excludedID, from, until).
		Select("COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 0 WHEN status = 'aborted' THEN actual_amount_kg ELSE planned_amount_kg END), 0)").
		Scan(&total).Error
	return total, err
}

// FindOpenRescheduleOf 返回已中止原记录当前仍未终态的补排记录（不存在返回 gorm.ErrRecordNotFound）。
func (r *ExecutionRepository) FindOpenRescheduleOf(sourceID uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	err := r.db.Where("reschedule_of_id = ? AND status IN ?", sourceID, []string{"scheduled", "running"}).First(&execution).Error
	return execution, err
}

func (r *ExecutionRepository) Create(execution *model.ControlExecution) error {
	return r.db.Create(execution).Error
}

func (r *ExecutionRepository) Save(execution *model.ControlExecution) error {
	// RescheduleOf 为只读关联展示，保存主记录时禁止 GORM 级联 upsert 关联记录。
	return r.db.Omit(clause.Associations).Save(execution).Error
}

func (r *ExecutionRepository) Delete(execution *model.ControlExecution) error {
	return r.db.Delete(execution).Error
}
