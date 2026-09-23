package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"math"
	"time"

	"gorm.io/gorm"
)

type ExecutionService struct {
	repo          *repository.ExecutionRepository
	plans         *repository.PlanRepository
	ponds         *repository.PondRepository
	readings      *repository.ReadingRepository
	audit         *AuditService
	transactional bool
}

func (s *ExecutionService) withinTransaction(fn func(*ExecutionService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		scoped := &ExecutionService{
			repo: repository.NewExecutionRepository(tx), plans: repository.NewPlanRepository(tx),
			ponds: repository.NewPondRepository(tx), readings: repository.NewReadingRepository(tx),
			audit: audit, transactional: true,
		}
		return fn(scoped)
	})
}

func NewExecutionService(repo *repository.ExecutionRepository, plans *repository.PlanRepository, ponds *repository.PondRepository, readings *repository.ReadingRepository, audit *AuditService) *ExecutionService {
	return &ExecutionService{repo: repo, plans: plans, ponds: ponds, readings: readings, audit: audit}
}

func (s *ExecutionService) List(query dto.PageQuery, pondID, planID uint) (dto.PageResult[model.ControlExecution], error) {
	query.Normalize()
	items, total, err := s.repo.List(query, pondID, planID)
	if err != nil {
		return dto.PageResult[model.ControlExecution]{}, WrapError(CodeInternal, "查询执行记录失败", err)
	}
	return dto.PageResult[model.ControlExecution]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *ExecutionService) Get(id uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	var err error
	if s.transactional {
		execution, err = s.repo.GetForUpdate(id)
	} else {
		execution, err = s.repo.Get(id)
	}
	if err == gorm.ErrRecordNotFound {
		return model.ControlExecution{}, NewError(CodeNotFound, "执行记录不存在")
	}
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "查询执行记录失败", err)
	}
	return execution, nil
}

func (s *ExecutionService) Create(input dto.ExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Create(input, actor)
			return inner
		})
		return result, err
	}
	plan, pond, latest, err := s.validateExecution(input.PondID, input.FeedingPlanID, input.PlannedAmountKg, input.ScheduledAt, 0)
	if err != nil {
		return model.ControlExecution{}, err
	}
	execution := model.ControlExecution{
		PondID: input.PondID, FeedingPlanID: input.FeedingPlanID, ScheduledAt: input.ScheduledAt.UTC(),
		PlannedAmountKg: input.PlannedAmountKg, Status: constants.ExecutionScheduled,
		Operator: actor.DisplayName, Weather: input.Weather, OxygenSnapshot: latest.DissolvedOxygen,
	}
	if execution.Operator == "" {
		execution.Operator = actor.Username
	}
	if err := s.repo.Create(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "创建执行记录失败", err)
	}
	execution.Pond = &pond
	execution.FeedingPlan = &plan
	if err := s.audit.Record(actor, "schedule", "control_execution", execution.ID, nil, execution, "根据已批准计划安排投喂"); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

func (s *ExecutionService) Update(id uint, input dto.UpdateExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Update(id, input, actor)
			return inner
		})
		return result, err
	}
	execution, err := s.Get(id)
	if err != nil {
		return model.ControlExecution{}, err
	}
	if execution.Status == constants.ExecutionCompleted || execution.Status == constants.ExecutionCancelled || execution.Status == constants.ExecutionAborted {
		return model.ControlExecution{}, NewError(CodeConflict, "已完成、已取消或已中止记录不能编辑")
	}
	if input.Status != constants.ExecutionScheduled && input.Status != constants.ExecutionRunning && input.Status != constants.ExecutionCancelled {
		return model.ControlExecution{}, NewError(CodeValidation, "执行状态无效")
	}
	if !execution.Status.CanTransitionTo(input.Status) {
		return model.ControlExecution{}, NewError(CodeConflict, "执行状态不允许逆向迁移")
	}
	if _, _, _, err := s.validateExecution(execution.PondID, execution.FeedingPlanID, input.PlannedAmountKg, input.ScheduledAt, execution.ID); err != nil && input.Status != constants.ExecutionCancelled {
		return model.ControlExecution{}, err
	}
	before := execution
	execution.ScheduledAt = input.ScheduledAt.UTC()
	execution.PlannedAmountKg = input.PlannedAmountKg
	execution.Weather = input.Weather
	if input.Status == constants.ExecutionRunning && execution.StartedAt == nil {
		now := time.Now().UTC()
		execution.StartedAt = &now
	}
	execution.Status = input.Status
	if err := s.repo.Save(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "更新执行记录失败", err)
	}
	if err := s.audit.Record(actor, "update", "control_execution", execution.ID, before, execution, "调整时间、数量或执行状态"); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

func (s *ExecutionService) Complete(id uint, input dto.CompleteExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Complete(id, input, actor)
			return inner
		})
		return result, err
	}
	execution, err := s.Get(id)
	if err != nil {
		return model.ControlExecution{}, err
	}
	if execution.Status != constants.ExecutionScheduled && execution.Status != constants.ExecutionRunning {
		return model.ControlExecution{}, NewError(CodeConflict, "当前执行状态不能提交反馈")
	}
	if input.OxygenSnapshot < execution.FeedingPlan.MinOxygen {
		return model.ControlExecution{}, NewError(CodeConflict, "现场溶解氧低于计划阈值，请停止投喂并处置水质")
	}
	if input.ActualAmountKg > execution.FeedingPlan.DailyAmountKg {
		return model.ControlExecution{}, NewError(CodeValidation, "单次实际投喂量不能超过计划日投喂量")
	}
	deviation := math.Abs(input.ActualAmountKg-execution.PlannedAmountKg) / execution.PlannedAmountKg
	if deviation > 0.25 && len([]rune(input.Feedback)) < 10 {
		return model.ControlExecution{}, NewError(CodeValidation, "实际量偏差超过 25% 时需提供至少 10 字说明")
	}
	before := execution
	now := time.Now().UTC()
	if execution.StartedAt == nil {
		execution.StartedAt = &now
	}
	execution.CompletedAt = &now
	execution.ActualAmountKg = input.ActualAmountKg
	execution.OxygenSnapshot = input.OxygenSnapshot
	execution.Feedback = input.Feedback
	execution.Status = constants.ExecutionCompleted
	if err := s.repo.Save(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "完成执行记录失败", err)
	}
	plan, err := s.plans.Get(execution.FeedingPlanID)
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "查询关联计划失败", err)
	}
	openCount, err := s.repo.CountOpenForPlanExcluding(plan.ID, execution.ID)
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "检查计划待执行记录失败", err)
	}
	if plan.Status == constants.PlanStatusApproved && openCount == 0 {
		planBefore := plan
		plan.Status = constants.PlanStatusExecuted
		if err := s.plans.Save(&plan); err != nil {
			return model.ControlExecution{}, WrapError(CodeInternal, "更新计划执行状态失败", err)
		}
		if err := s.audit.Record(actor, "execute", "feeding_plan", plan.ID, planBefore, plan, "首次投喂执行已完成"); err != nil {
			return model.ControlExecution{}, err
		}
	}
	if err := s.audit.Record(actor, "complete", "control_execution", execution.ID, before, execution, input.Feedback); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

// Abort 异常中止待执行或执行中的记录：登记中止原因与实际已投喂量。
// 中止后记录仅按实际量占用当日额度，原计划量未投喂部分释放，可供补排。
func (s *ExecutionService) Abort(id uint, input dto.AbortExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Abort(id, input, actor)
			return inner
		})
		return result, err
	}
	execution, err := s.Get(id)
	if err != nil {
		return model.ControlExecution{}, err
	}
	if execution.Status != constants.ExecutionScheduled && execution.Status != constants.ExecutionRunning {
		return model.ControlExecution{}, NewError(CodeConflict, "只有待执行或执行中的记录可以异常中止")
	}
	if len([]rune(input.AbortReason)) < 2 {
		return model.ControlExecution{}, NewError(CodeValidation, "请填写中止原因")
	}
	if input.ActualAmountKg > execution.PlannedAmountKg+0.0001 {
		return model.ControlExecution{}, NewError(CodeValidation, "已投喂量不能超过本次计划量")
	}
	before := execution
	now := time.Now().UTC()
	if execution.Status == constants.ExecutionRunning && execution.StartedAt == nil {
		execution.StartedAt = &now
	}
	execution.AbortedAt = &now
	execution.ActualAmountKg = input.ActualAmountKg
	execution.AbortReason = input.AbortReason
	execution.Status = constants.ExecutionAborted
	if err := s.repo.Save(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "中止执行记录失败", err)
	}
	if err := s.audit.Record(actor, "abort", "control_execution", execution.ID, before, execution, "异常中止："+input.AbortReason); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

// Reschedule 仅允许从已中止记录发起补排，补排量不得超过当日计划日量的未达差额。
func (s *ExecutionService) Reschedule(sourceID uint, input dto.RescheduleExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Reschedule(sourceID, input, actor)
			return inner
		})
		return result, err
	}
	source, err := s.Get(sourceID)
	if err != nil {
		return model.ControlExecution{}, err
	}
	if source.Status != constants.ExecutionAborted {
		return model.ControlExecution{}, NewError(CodeConflict, "只能对已中止记录发起补排")
	}
	scheduledAt := input.ScheduledAt.UTC()
	sourceDay := dayStart(source.ScheduledAt)
	plan, err := s.plans.GetForUpdate(source.FeedingPlanID)
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "查询投喂计划失败", err)
	}
	if dayStart(scheduledAt) != sourceDay {
		return model.ControlExecution{}, NewError(CodeValidation, "补排必须安排在原中止记录的计划当日")
	}
	if plan.Status != constants.PlanStatusApproved {
		return model.ControlExecution{}, NewError(CodeConflict, "投喂计划已结束，不能补排")
	}
	occupied, err := s.repo.OccupiedAmountForDay(source.PondID, sourceDay, sourceDay.Add(24*time.Hour), 0)
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "核算当日投喂额度失败", err)
	}
	available := plan.DailyAmountKg - occupied
	if available <= 0.0001 {
		return model.ControlExecution{}, NewError(CodeConflict, "当日计划日量已投喂完毕，余额为零，不能补排")
	}
	if input.PlannedAmountKg > available+0.0001 {
		return model.ControlExecution{}, NewError(CodeConflict, "补排量超过当日未达差额，余额不足")
	}
	if existing, findErr := s.repo.FindOpenRescheduleOf(sourceID); findErr == nil && existing.ID != 0 {
		return model.ControlExecution{}, NewError(CodeConflict, "该中止记录已有待执行或执行中的补排安排")
	} else if findErr != nil && findErr != gorm.ErrRecordNotFound {
		return model.ControlExecution{}, WrapError(CodeInternal, "检查既有补排安排失败", findErr)
	}
	// 补排仍需通过水质、溶氧、养殖池状态与计划周期等既有安排校验。
	_, _, latest, validationErr := s.validateExecution(source.PondID, source.FeedingPlanID, input.PlannedAmountKg, scheduledAt, 0)
	if validationErr != nil {
		return model.ControlExecution{}, validationErr
	}
	execution := model.ControlExecution{
		PondID: source.PondID, FeedingPlanID: source.FeedingPlanID, ScheduledAt: scheduledAt,
		PlannedAmountKg: input.PlannedAmountKg, Status: constants.ExecutionScheduled,
		Operator: actor.DisplayName, Weather: input.Weather, OxygenSnapshot: latest.DissolvedOxygen,
		RescheduleOfID: &sourceID,
	}
	if execution.Operator == "" {
		execution.Operator = actor.Username
	}
	if err := s.repo.Create(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "创建补排安排失败", err)
	}
	if err := s.audit.Record(actor, "reschedule", "control_execution", execution.ID, nil, execution, "根据中止记录发起补排"); err != nil {
		return model.ControlExecution{}, err
	}
	// 补排关系仅存于新记录的 reschedule_of_id；原中止记录保持 aborted 终态，
	// 记录一条关系审计，便于从原记录追溯关联的补排安排。
	if err := s.audit.Record(actor, "reschedule_link", "control_execution", source.ID, source, execution, "中止记录已关联补排安排"); err != nil {
		return model.ControlExecution{}, err
	}
	return s.Get(execution.ID)
}

func (s *ExecutionService) Delete(id uint, actor Actor) error {
	if !s.transactional {
		return s.withinTransaction(func(scoped *ExecutionService) error { return scoped.Delete(id, actor) })
	}
	execution, err := s.Get(id)
	if err != nil {
		return err
	}
	if execution.Status != constants.ExecutionScheduled {
		return NewError(CodeConflict, "只有待执行记录可以删除")
	}
	// 若删除的是补排安排，保留原中止记录的中止状态，仅解除补排关系以便重新补排。
	if execution.RescheduleOfID != nil {
		source, err := s.Get(*execution.RescheduleOfID)
		if err != nil {
			return err
		}
		before := source
		if err := s.audit.Record(actor, "reschedule_unlink", "control_execution", source.ID, before, source, "补排安排删除，解除与中止记录的关联"); err != nil {
			return err
		}
	}
	if err := s.repo.Delete(&execution); err != nil {
		return WrapError(CodeInternal, "删除执行记录失败", err)
	}
	return s.audit.Record(actor, "delete", "control_execution", execution.ID, execution, nil, "取消未开始的投喂安排")
}

func dayStart(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *ExecutionService) validateExecution(pondID, planID uint, amount float64, scheduledAt time.Time, excludedID uint) (model.FeedingPlan, model.Pond, model.WaterReading, error) {
	var plan model.FeedingPlan
	var err error
	if s.transactional {
		plan, err = s.plans.GetForUpdate(planID)
	} else {
		plan, err = s.plans.Get(planID)
	}
	if err == gorm.ErrRecordNotFound {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "投喂计划不存在")
	}
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "查询投喂计划失败", err)
	}
	if plan.PondID != pondID {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "投喂计划与养殖池不匹配")
	}
	if plan.Status != constants.PlanStatusApproved {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "只能使用已批准计划安排执行")
	}
	if scheduledAt.Before(plan.StartDate) || scheduledAt.After(plan.EndDate.Add(24*time.Hour)) {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "执行时间必须在计划周期内")
	}
	if amount > plan.DailyAmountKg {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "单次计划量不能超过日投喂量")
	}
	var pond model.Pond
	if s.transactional {
		pond, err = s.ponds.GetForUpdate(pondID)
	} else {
		pond, err = s.ponds.Get(pondID)
	}
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "查询养殖池失败", err)
	}
	if pond.Status != constants.PondStatusActive {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "养殖池非运行状态，不能安排投喂")
	}
	latest, err := s.readings.LatestForPond(pondID)
	if err == gorm.ErrRecordNotFound {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "安排执行前必须有水质读数")
	}
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "查询最新水质读数失败", err)
	}
	if time.Since(latest.MeasuredAt) > 24*time.Hour {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "最新水质读数已超过 24 小时")
	}
	if latest.DissolvedOxygen < plan.MinOxygen || latest.RiskLevel == constants.RiskCritical {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "当前水质不满足计划执行条件")
	}
	dayStart := dayStart(scheduledAt)
	occupiedForDay, err := s.repo.OccupiedAmountForDay(pondID, dayStart, dayStart.Add(24*time.Hour), excludedID)
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "核算当日投喂安排失败", err)
	}
	if occupiedForDay+amount > plan.DailyAmountKg+0.0001 {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "当日累计安排不能超过计划日投喂量")
	}
	return plan, pond, latest, nil
}
