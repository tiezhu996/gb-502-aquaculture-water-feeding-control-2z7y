<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleCheck, Clock, List, Plus, RefreshRight, VideoPlay } from '@element-plus/icons-vue'
import { executionApi } from '@/api/executions'
import { planApi } from '@/api/plans'
import { pondApi } from '@/api/ponds'
import MetricCard from '@/components/common/MetricCard.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import PlanDrawer from '@/components/common/PlanDrawer.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAuth } from '@/hooks/useAuth'
import { useQueryParams } from '@/hooks/useQueryParams'
import type { ControlExecution, ExecutionInput, FeedingPlan, Pond } from '@/types/models'
import { errorMessage } from '@/utils/errors'
import { formatDateTime, formatNumber, toISO, toLocalInput } from '@/utils/format'

const { canOperate } = useAuth()
const { params } = useQueryParams({ status: '', pondId: '', page: 1 })
const executions = ref<ControlExecution[]>([])
const ponds = ref<Pond[]>([])
const plans = ref<FeedingPlan[]>([])
const total = ref(0)
const loading = ref(false)
const saving = ref(false)
const editorOpen = ref(false)
const completeOpen = ref(false)
const abortOpen = ref(false)
const rescheduleOpen = ref(false)
const deleteOpen = ref(false)
const drawerOpen = ref(false)
const target = ref<ControlExecution | null>(null)
const selectedPlan = ref<FeedingPlan | null>(null)
const scheduledLocal = ref(toLocalInput(new Date(Date.now() + 3600000)))
const form = reactive<ExecutionInput>({ pondId: 0, feedingPlanId: 0, scheduledAt: '', plannedAmountKg: 0, weather: '' })
const completion = reactive({ actualAmountKg: 0, oxygenSnapshot: 6, feedback: '' })
const abortForm = reactive({ actualAmountKg: 0, abortReason: '' })
const rescheduleLocal = ref(toLocalInput())
const rescheduleForm = reactive({ scheduledAt: '', plannedAmountKg: 0, weather: '' })

const scheduledCount = computed(() => executions.value.filter((item) => item.status === 'scheduled').length)
const runningCount = computed(() => executions.value.filter((item) => item.status === 'running').length)
const completedAmount = computed(() => executions.value
  .filter((item) => item.status === 'completed' || item.status === 'aborted')
  .reduce((sum, item) => sum + item.actualAmountKg, 0))
const availablePlans = computed(() => plans.value.filter((plan) => plan.status === 'approved' && (!form.pondId || plan.pondId === form.pondId)))

// 中止记录未达计划量的差额，即补排允许使用的上限；为零时不允许发起补排。
const rescheduleShortfall = computed(() => {
  if (!target.value) return 0
  return Math.max(0, Number((target.value.plannedAmountKg - target.value.actualAmountKg).toFixed(3)))
})

async function load() {
  loading.value = true
  try {
    const [result, pondResult, planResult] = await Promise.all([
      executionApi.list({ page: Number(params.page), pageSize: 20, status: String(params.status), pondId: Number(params.pondId) || undefined }),
      pondApi.list({ page: 1, pageSize: 100 }),
      planApi.list({ page: 1, pageSize: 100 }),
    ])
    executions.value = result.items
    total.value = result.total
    ponds.value = pondResult.items
    plans.value = planResult.items
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  const firstPlan = plans.value.find((item) => item.status === 'approved')
  Object.assign(form, {
    pondId: firstPlan?.pondId || 0, feedingPlanId: firstPlan?.id || 0, plannedAmountKg: firstPlan ? firstPlan.dailyAmountKg / firstPlan.frequencyPerDay : 0, weather: '晴朗，微风',
  })
  scheduledLocal.value = toLocalInput(new Date(Date.now() + 3600000))
  editorOpen.value = true
}

function onPlanChange(planId: number) {
  const plan = plans.value.find((item) => item.id === planId)
  if (!plan) return
  form.pondId = plan.pondId
  form.plannedAmountKg = Number((plan.dailyAmountKg / plan.frequencyPerDay).toFixed(2))
}

async function create() {
  if (!form.pondId || !form.feedingPlanId || form.plannedAmountKg <= 0) {
    ElMessage.warning('请选择已批准计划并填写数量')
    return
  }
  saving.value = true
  try {
    await executionApi.create({ ...form, scheduledAt: toISO(scheduledLocal.value) })
    ElMessage.success('投喂执行已安排')
    editorOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function start(execution: ControlExecution) {
  saving.value = true
  try {
    await executionApi.update(execution.id, {
      scheduledAt: execution.scheduledAt, plannedAmountKg: execution.plannedAmountKg, weather: execution.weather, status: 'running',
    })
    ElMessage.success('执行已开始')
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function openComplete(execution: ControlExecution) {
  target.value = execution
  Object.assign(completion, { actualAmountKg: execution.plannedAmountKg, oxygenSnapshot: execution.oxygenSnapshot || 6, feedback: '' })
  completeOpen.value = true
}

async function complete() {
  if (!target.value || completion.actualAmountKg <= 0 || completion.feedback.trim().length < 2) {
    ElMessage.warning('请完整填写实际数量、现场溶解氧和反馈')
    return
  }
  saving.value = true
  try {
    await executionApi.complete(target.value.id, { ...completion })
    ElMessage.success('执行反馈已提交，计划状态已同步')
    completeOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!target.value) return
  saving.value = true
  try {
    await executionApi.remove(target.value.id)
    ElMessage.success('待执行安排已删除')
    deleteOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function openAbort(execution: ControlExecution) {
  target.value = execution
  Object.assign(abortForm, { actualAmountKg: 0, abortReason: '' })
  abortOpen.value = true
}

async function abort() {
  if (!target.value) return
  if (abortForm.actualAmountKg < 0 || abortForm.actualAmountKg > target.value.plannedAmountKg) {
    ElMessage.warning('已投喂量必须在 0 与本次计划量之间')
    return
  }
  if (abortForm.abortReason.trim().length < 2) {
    ElMessage.warning('请填写中止原因')
    return
  }
  saving.value = true
  try {
    await executionApi.abort(target.value.id, {
      actualAmountKg: abortForm.actualAmountKg,
      abortReason: abortForm.abortReason.trim(),
    })
    ElMessage.success('执行已中止，实际量计入当日累计')
    abortOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function openReschedule(execution: ControlExecution) {
  target.value = execution
  const shortfall = Math.max(0, Number((execution.plannedAmountKg - execution.actualAmountKg).toFixed(3)))
  Object.assign(rescheduleForm, { scheduledAt: '', plannedAmountKg: shortfall, weather: execution.weather || '' })
  rescheduleLocal.value = toLocalInput(execution.scheduledAt)
  rescheduleOpen.value = true
}

async function reschedule() {
  if (!target.value) return
  if (rescheduleShortfall.value <= 0) {
    ElMessage.warning('中止记录已达到计划日量，没有可补排差额')
    return
  }
  if (rescheduleForm.plannedAmountKg <= 0 || rescheduleForm.plannedAmountKg > rescheduleShortfall.value) {
    ElMessage.warning(`补排量只能在 0 与未投喂差额 ${formatNumber(rescheduleShortfall.value, 2)} kg 之间`)
    return
  }
  saving.value = true
  try {
    await executionApi.reschedule(target.value.id, {
      ...rescheduleForm,
      scheduledAt: toISO(rescheduleLocal.value),
    })
    ElMessage.success('补排安排已生成并与中止记录关联')
    rescheduleOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function focusRow(execution?: ControlExecution | null) {
  if (!execution) return
  params.status = execution.status
  params.pondId = String(execution.pondId)
}

function showPlan(execution: ControlExecution) {
  selectedPlan.value = execution.feedingPlan || plans.value.find((item) => item.id === execution.feedingPlanId) || null
  drawerOpen.value = true
}

let timer: number | undefined
watch(params, () => { window.clearTimeout(timer); timer = window.setTimeout(load, 200) }, { deep: true })
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <section class="metrics-grid">
      <MetricCard label="执行记录" :value="total" :icon="List" />
      <MetricCard label="待执行" :value="scheduledCount" :icon="Clock" tone="amber" />
      <MetricCard label="执行中" :value="runningCount" :icon="VideoPlay" tone="blue" />
      <MetricCard label="页内已投喂" :value="`${formatNumber(completedAmount)} kg`" :icon="CircleCheck" tone="green" />
    </section>
    <section class="workspace-panel">
      <div class="panel-toolbar">
        <div class="filters">
          <el-select v-model="params.pondId" placeholder="全部养殖池" clearable><el-option v-for="pond in ponds" :key="pond.id" :label="pond.name" :value="String(pond.id)" /></el-select>
          <el-select v-model="params.status" placeholder="全部状态" clearable><el-option label="待执行" value="scheduled" /><el-option label="执行中" value="running" /><el-option label="已完成" value="completed" /><el-option label="已中止" value="aborted" /><el-option label="已取消" value="cancelled" /></el-select>
        </div>
        <el-button v-if="canOperate()" type="primary" :icon="Plus" @click="openCreate">安排执行</el-button>
      </div>
      <el-table v-loading="loading" :data="executions" stripe empty-text="暂无执行记录">
        <el-table-column label="养殖池 / 计划" min-width="230"><template #default="{ row }"><div class="primary-cell"><strong>{{ row.pond?.name }}</strong><button class="inline-link" @click="showPlan(row)">{{ row.feedingPlan?.name }} · v{{ row.feedingPlan?.version }}</button></div></template></el-table-column>
        <el-table-column label="安排时间" min-width="165"><template #default="{ row }">{{ formatDateTime(row.scheduledAt) }}</template></el-table-column>
        <el-table-column label="计划 / 实际" min-width="130"><template #default="{ row }">{{ row.plannedAmountKg }} / {{ row.actualAmountKg || '—' }} kg</template></el-table-column>
        <el-table-column label="中止 / 补排" min-width="200">
          <template #default="{ row }">
            <div v-if="row.status === 'aborted'" class="abort-cell">
              <el-tag type="warning" effect="plain" size="small">已中止</el-tag>
              <span class="abort-reason" :title="row.abortReason">{{ row.abortReason }}</span>
            </div>
            <div v-if="row.rescheduledFrom" class="link-line">
              <el-button link type="primary" size="small" @click="focusRow(row.rescheduledFrom)">来源中止 #{{ row.rescheduledFrom.id }}</el-button>
            </div>
            <div v-else-if="row.rescheduledTo" class="link-line">
              <el-button link type="primary" size="small" @click="focusRow(row.rescheduledTo)">补排安排 #{{ row.rescheduledTo.id }}</el-button>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="天气" prop="weather" min-width="130" show-overflow-tooltip />
        <el-table-column label="操作人" prop="operator" width="110" />
        <el-table-column label="状态" width="100"><template #default="{ row }"><StatusBadge :status="row.status" /></template></el-table-column>
        <el-table-column v-if="canOperate()" label="操作" width="270" fixed="right"><template #default="{ row }">
          <el-button v-if="row.status === 'scheduled'" link type="primary" :loading="saving" @click="start(row)">开始</el-button>
          <el-button v-if="row.status === 'scheduled' || row.status === 'running'" link type="success" @click="openComplete(row)">提交反馈</el-button>
          <el-button v-if="row.status === 'scheduled' || row.status === 'running'" link type="warning" @click="openAbort(row)">中止</el-button>
          <el-button v-if="row.status === 'aborted' && !row.rescheduledTo" link type="primary" :icon="RefreshRight" @click="openReschedule(row)">补排</el-button>
          <el-button v-if="row.status === 'aborted' && row.rescheduledTo" link type="info" disabled :icon="RefreshRight">已补排</el-button>
          <el-button v-if="row.status === 'scheduled'" link type="danger" @click="target = row; deleteOpen = true">删除</el-button>
        </template></el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="params.page" layout="total, prev, pager, next" :total="total" :page-size="20" /></div>
    </section>
    <el-dialog v-model="editorOpen" title="安排投喂执行" width="620px">
      <el-alert title="仅可选择已批准计划；保存时将检查 24 小时内水质" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="养殖池"><el-select v-model="form.pondId" @change="form.feedingPlanId = 0"><el-option v-for="pond in ponds.filter((item) => item.status === 'active')" :key="pond.id" :label="pond.name" :value="pond.id" /></el-select></el-form-item>
        <el-form-item label="已批准计划"><el-select v-model="form.feedingPlanId" @change="onPlanChange"><el-option v-for="plan in availablePlans" :key="plan.id" :label="`${plan.name} · v${plan.version}`" :value="plan.id" /></el-select></el-form-item>
        <el-form-item label="执行时间"><el-date-picker v-model="scheduledLocal" type="datetime" value-format="YYYY-MM-DDTHH:mm" /></el-form-item>
        <el-form-item label="计划数量（kg）"><el-input-number v-model="form.plannedAmountKg" :min="0.1" :step="1" /></el-form-item>
        <el-form-item label="天气窗口" class="form-span"><el-input v-model="form.weather" placeholder="例：晴朗，微风" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="create">确认安排</el-button></template>
    </el-dialog>
    <el-dialog v-model="completeOpen" title="提交执行反馈" width="580px">
      <el-form label-position="top" class="form-grid">
        <el-form-item label="实际投喂量（kg）"><el-input-number v-model="completion.actualAmountKg" :min="0.1" :step="0.5" /></el-form-item>
        <el-form-item label="现场溶解氧（mg/L）"><el-input-number v-model="completion.oxygenSnapshot" :min="0" :max="30" :step="0.1" /></el-form-item>
        <el-form-item label="执行反馈" class="form-span"><el-input v-model="completion.feedback" type="textarea" :rows="4" placeholder="记录摄食、设备与异常情况" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="completeOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="complete">完成并留痕</el-button></template>
    </el-dialog>
    <ConfirmDialog v-model="deleteOpen" title="删除执行安排" message="只能删除尚未开始的执行安排，确认继续？" danger :loading="saving" @confirm="remove" />
    <el-dialog v-model="abortOpen" title="中止投喂执行" width="560px">
      <el-alert type="warning" :closable="false" show-icon class="form-alert"
        :title="`中止后该记录进入终态，按实际投喂量计入当日累计，原计划量 ${target?.plannedAmountKg ?? 0} kg 不再占用。`" />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="已投喂量（kg）"><el-input-number v-model="abortForm.actualAmountKg" :min="0" :max="target?.plannedAmountKg || 0" :step="0.5" /></el-form-item>
        <el-form-item label="中止原因" class="form-span"><el-input v-model="abortForm.abortReason" type="textarea" :rows="4" placeholder="说明设备、水质或鱼群异常等中止原因（至少 2 个字）" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="abortOpen = false">取消</el-button><el-button type="warning" :loading="saving" @click="abort">确认中止</el-button></template>
    </el-dialog>
    <el-dialog v-model="rescheduleOpen" title="补排投喂安排" width="560px">
      <el-alert v-if="rescheduleShortfall > 0" type="info" :closable="false" show-icon class="form-alert"
        :title="`仅可补排未达计划量的差额 ${formatNumber(rescheduleShortfall, 2)} kg，且不能超过当日剩余余额。`" />
      <el-alert v-else type="error" :closable="false" show-icon class="form-alert" title="该中止记录已达到计划日量，差额为零，不能补排。" />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="补排时间（须与原安排同一日）"><el-date-picker v-model="rescheduleLocal" type="datetime" value-format="YYYY-MM-DDTHH:mm" /></el-form-item>
        <el-form-item label="补排量（kg）"><el-input-number v-model="rescheduleForm.plannedAmountKg" :min="0.1" :max="rescheduleShortfall || undefined" :step="0.5" :disabled="rescheduleShortfall <= 0" /></el-form-item>
        <el-form-item label="天气窗口" class="form-span"><el-input v-model="rescheduleForm.weather" placeholder="例：晴朗，微风" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="rescheduleOpen = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="rescheduleShortfall <= 0" @click="reschedule">生成补排安排</el-button></template>
    </el-dialog>
    <PlanDrawer v-model="drawerOpen" :plan="selectedPlan" />
  </div>
</template>

<style scoped>
.abort-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}
.abort-reason {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.link-line {
  margin-top: 2px;
}
.form-alert {
  margin-bottom: 16px;
}
</style>
