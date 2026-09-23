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
	err := base.Preload("Pond").Preload("FeedingPlan").Order("scheduled_at DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&executions).Error
	if err != nil {
		return executions, total, err
	}
	if err := r.attachLinks(executions); err != nil {
		return executions, total, err
	}
	return executions, total, err
}

func (r *ExecutionRepository) Get(id uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	err := r.db.Preload("Pond").Preload("FeedingPlan").First(&execution, id).Error
	if err == nil {
		err = r.attachLink(&execution)
	}
	return execution, err
}

func (r *ExecutionRepository) GetForUpdate(id uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Pond").Preload("FeedingPlan").First(&execution, id).Error
	if err == nil {
		err = r.attachLink(&execution)
	}
	return execution, err
}

// linkColumn 返回被挂载方向的列名：补排记录展示其来源（rescheduled_from_id），
// 中止记录展示其补排（rescheduled_to_id）。只单向加载一层以避免循环。
func (r *ExecutionRepository) attachLinks(executions []model.ControlExecution) error {
	fromIDs := map[uint]struct{}{}
	toIDs := map[uint]struct{}{}
	for _, execution := range executions {
		if execution.RescheduledFromID != nil {
			fromIDs[*execution.RescheduledFromID] = struct{}{}
		}
		if execution.RescheduledToID != nil {
			toIDs[*execution.RescheduledToID] = struct{}{}
		}
	}
	links := map[uint]*model.ControlExecution{}
	allIDs := make([]uint, 0, len(fromIDs)+len(toIDs))
	for id := range fromIDs {
		allIDs = append(allIDs, id)
	}
	for id := range toIDs {
		allIDs = append(allIDs, id)
	}
	if len(allIDs) > 0 {
		var related []model.ControlExecution
		if err := r.db.Preload("Pond").Where("id IN ?", allIDs).Find(&related).Error; err != nil {
			return err
		}
		for i := range related {
			links[related[i].ID] = &related[i]
		}
	}
	for i := range executions {
		if executions[i].RescheduledFromID != nil {
			executions[i].RescheduledFrom = links[*executions[i].RescheduledFromID]
		}
		if executions[i].RescheduledToID != nil {
			executions[i].RescheduledTo = links[*executions[i].RescheduledToID]
		}
	}
	return nil
}

func (r *ExecutionRepository) attachLink(execution *model.ControlExecution) error {
	list := []model.ControlExecution{*execution}
	if err := r.attachLinks(list); err != nil {
		return err
	}
	execution.RescheduledFrom = list[0].RescheduledFrom
	execution.RescheduledTo = list[0].RescheduledTo
	return nil
}

func (r *ExecutionRepository) CountOpenForPlanExcluding(planID, excludedID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.ControlExecution{}).
		Where("feeding_plan_id = ? AND id <> ? AND status IN ?", planID, excludedID, []string{"scheduled", "running"}).Count(&count).Error
	return count, err
}

// PlannedAmountForDay 核算当日已占用的投喂量：
// 已取消记录不占用；中止记录只按实际投喂量计入；其余记录按计划量占用。
func (r *ExecutionRepository) PlannedAmountForDay(pondID uint, from, until time.Time, excludedID uint) (float64, error) {
	var total float64
	err := r.db.Model(&model.ControlExecution{}).
		Where("pond_id = ? AND id <> ? AND scheduled_at >= ? AND scheduled_at < ? AND status <> ?", pondID, excludedID, from, until, "cancelled").
		Select("COALESCE(SUM(CASE WHEN status = 'aborted' THEN actual_amount_kg ELSE planned_amount_kg END), 0)").Scan(&total).Error
	return total, err
}

func (r *ExecutionRepository) Create(execution *model.ControlExecution) error {
	return r.db.Create(execution).Error
}

func (r *ExecutionRepository) Save(execution *model.ControlExecution) error {
	// 关联（养殖池、计划、中止/补排关系）由业务显式维护，避免 GORM 保存时级联改写关联实体。
	return r.db.Omit(clause.Associations).Save(execution).Error
}

func (r *ExecutionRepository) Delete(execution *model.ControlExecution) error {
	return r.db.Delete(execution).Error
}
