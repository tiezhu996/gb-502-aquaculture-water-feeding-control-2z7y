<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleCheck, Clock, List, Plus, VideoPlay, Warning } from '@element-plus/icons-vue'
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
const abortion = reactive({ actualAmountKg: 0, abortReason: '' })
const rescheduleLocal = ref(toLocalInput(new Date(Date.now() + 3600000)))
const rescheduleForm = reactive({ plannedAmountKg: 0, weather: '' })

const scheduledCount = computed(() => executions.value.filter((item) => item.status === 'scheduled').length)
const runningCount = computed(() => executions.value.filter((item) => item.status === 'running').length)
const fedAmount = computed(() => executions.value
  .filter((item) => item.status === 'completed' || item.status === 'aborted')
  .reduce((sum, item) => sum + item.actualAmountKg, 0))
const availablePlans = computed(() => plans.value.filter((plan) => plan.status === 'approved' && (!form.pondId || plan.pondId === form.pondId)))

function utcDayKey(value: string): string {
  const date = new Date(value)
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate())).toISOString()
}

// 当日余额（页内估算，最终以服务端核算为准）：计划日量 - 当日已占用。
// 中止记录按实际已投喂量占用，取消记录不占用，其余按计划量占用。
function dailyBalance(execution: ControlExecution): { occupied: number; daily: number; balance: number } {
  const plan = execution.feedingPlan || plans.value.find((item) => item.id === execution.feedingPlanId)
  const daily = plan?.dailyAmountKg || 0
  const dayKey = utcDayKey(execution.scheduledAt)
  const occupied = executions.value
    .filter((item) => item.pondId === execution.pondId && utcDayKey(item.scheduledAt) === dayKey)
    .reduce((sum, item) => {
      if (item.status === 'cancelled') return sum
      if (item.status === 'aborted') return sum + item.actualAmountKg
      return sum + item.plannedAmountKg
    }, 0)
  return { occupied, daily, balance: Math.max(0, daily - occupied) }
}

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

function openAbort(execution: ControlExecution) {
  target.value = execution
  Object.assign(abortion, { actualAmountKg: 0, abortReason: '' })
  abortOpen.value = true
}

async function abort() {
  if (!target.value || abortion.abortReason.trim().length < 2) {
    ElMessage.warning('请填写中止原因，并确认已投喂量')
    return
  }
  if (abortion.actualAmountKg > target.value.plannedAmountKg) {
    ElMessage.warning('已投喂量不能超过本次计划量')
    return
  }
  saving.value = true
  try {
    await executionApi.abort(target.value.id, { ...abortion })
    ElMessage.success('执行已中止，实际量已计入当日累计')
    abortOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

const rescheduleBalance = computed(() => target.value ? dailyBalance(target.value) : { occupied: 0, daily: 0, balance: 0 })

function openReschedule(execution: ControlExecution) {
  target.value = execution
  const balance = dailyBalance(execution)
  Object.assign(rescheduleForm, { plannedAmountKg: Number(balance.balance.toFixed(2)), weather: execution.weather || '晴朗，微风' })
  rescheduleLocal.value = toLocalInput(new Date(execution.scheduledAt))
  rescheduleOpen.value = true
}

async function reschedule() {
  if (!target.value) return
  if (rescheduleForm.plannedAmountKg <= 0) {
    ElMessage.warning('补排量必须大于 0')
    return
  }
  saving.value = true
  try {
    await executionApi.reschedule(target.value.id, {
      scheduledAt: toISO(rescheduleLocal.value), plannedAmountKg: rescheduleForm.plannedAmountKg, weather: rescheduleForm.weather,
    })
    ElMessage.success('补排安排已生成，并与中止记录关联')
    rescheduleOpen.value = false
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
    ElMessage.success(target.value.rescheduleOfId ? '补排安排已删除，原中止记录可重新补排' : '待执行安排已删除')
    deleteOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function showPlan(execution: ControlExecution) {
  selectedPlan.value = execution.feedingPlan || plans.value.find((item) => item.id === execution.feedingPlanId) || null
  drawerOpen.value = true
}

function locateExecution(execution?: ControlExecution | null) {
  if (!execution) return
  params.pondId = String(execution.pondId)
  params.status = execution.status
  params.page = 1
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
      <MetricCard label="页内已投喂" :value="`${formatNumber(fedAmount)} kg`" :icon="CircleCheck" tone="green" />
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
        <el-table-column label="养殖池 / 计划" min-width="240"><template #default="{ row }"><div class="primary-cell"><strong>{{ row.pond?.name }}</strong><button class="inline-link" @click="showPlan(row)">{{ row.feedingPlan?.name }} · v{{ row.feedingPlan?.version }}</button><el-tag v-if="row.rescheduleOfId" size="small" type="warning" effect="plain" round class="relation-tag">补排自 #{{ row.rescheduleOfId }}</el-tag><el-tag v-else-if="row.rescheduledTo" size="small" type="primary" effect="plain" round class="relation-tag">已补排 #{{ row.rescheduledTo.id }}</el-tag></div></template></el-table-column>
        <el-table-column label="安排时间" min-width="165"><template #default="{ row }">{{ formatDateTime(row.scheduledAt) }}</template></el-table-column>
        <el-table-column label="计划 / 实际" min-width="140"><template #default="{ row }"><span :class="{ 'aborted-amount': row.status === 'aborted' }">{{ row.plannedAmountKg }} / {{ row.actualAmountKg || '—' }} kg</span></template></el-table-column>
        <el-table-column label="天气" prop="weather" min-width="120" show-overflow-tooltip />
        <el-table-column label="操作人" prop="operator" width="100" />
        <el-table-column label="状态" width="100"><template #default="{ row }"><StatusBadge :status="row.status" /></template></el-table-column>
        <el-table-column v-if="canOperate()" label="操作" width="250" fixed="right"><template #default="{ row }">
          <el-button v-if="row.status === 'scheduled'" link type="primary" :loading="saving" @click="start(row)">开始</el-button>
          <el-button v-if="row.status === 'scheduled' || row.status === 'running'" link type="success" @click="openComplete(row)">提交反馈</el-button>
          <el-button v-if="row.status === 'scheduled' || row.status === 'running'" link type="warning" @click="openAbort(row)">异常中止</el-button>
          <el-button v-if="row.status === 'aborted' && !row.rescheduledTo" link type="primary" :disabled="dailyBalance(row).balance <= 0" @click="openReschedule(row)">补排</el-button>
          <el-button v-if="row.rescheduledTo" link type="primary" @click="locateExecution(row.rescheduledTo)">查看补排 #{{ row.rescheduledTo.id }}</el-button>
          <el-button v-if="row.status === 'scheduled'" link type="danger" @click="target = row; deleteOpen = true">删除</el-button>
        </template></el-table-column>
      </el-table>
      <el-table v-if="executions.some((item) => item.status === 'aborted')" :data="executions.filter((item) => item.status === 'aborted')" size="small" class="abort-detail" empty-text="">
        <el-table-column label="中止记录" width="110"><template #default="{ row }">#{{ row.id }}（{{ row.pond?.name }}）</template></el-table-column>
        <el-table-column label="中止时间" width="165"><template #default="{ row }">{{ formatDateTime(row.abortedAt) }}</template></el-table-column>
        <el-table-column label="已投喂 / 当日余额" width="180"><template #default="{ row }">{{ row.actualAmountKg }} kg / {{ formatNumber(dailyBalance(row).balance, 2) }} kg</template></el-table-column>
        <el-table-column label="中止原因" prop="abortReason" min-width="220" show-overflow-tooltip />
        <el-table-column label="补排关系" width="160"><template #default="{ row }"><span v-if="row.rescheduledTo"><button class="inline-link" @click="locateExecution(row.rescheduledTo)">补排 #{{ row.rescheduledTo.id }}（待执行）</button></span><span v-else-if="dailyBalance(row).balance <= 0" class="balance-zero">当日余额为零</span><span v-else>尚未补排</span></template></el-table-column>
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
    <el-dialog v-model="abortOpen" title="异常中止投喂" width="580px">
      <el-alert title="中止后该记录按实际已投喂量计入当日累计，未投喂部分释放；仅可从中止记录发起补排。" type="warning" :closable="false" show-icon :icon="Warning" />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="已投喂量（kg）"><el-input-number v-model="abortion.actualAmountKg" :min="0" :max="target?.plannedAmountKg" :step="0.5" /></el-form-item>
        <el-form-item label="中止时间"><el-input :model-value="formatDateTime(target?.abortedAt || undefined)" disabled placeholder="保存时记录" /></el-form-item>
        <el-form-item label="中止原因" class="form-span"><el-input v-model="abortion.abortReason" type="textarea" :rows="4" placeholder="如设备故障、溶解氧骤降、鱼群异常等，至少 2 个字" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="abortOpen = false">取消</el-button><el-button type="warning" :loading="saving" @click="abort">确认中止</el-button></template>
    </el-dialog>
    <el-dialog v-model="rescheduleOpen" title="中止补排" width="580px">
      <el-alert v-if="rescheduleBalance.balance > 0" :title="`当日计划日量 ${formatNumber(rescheduleBalance.daily, 2)} kg，已占用 ${formatNumber(rescheduleBalance.occupied, 2)} kg，可补排差额 ${formatNumber(rescheduleBalance.balance, 2)} kg`" type="info" :closable="false" show-icon />
      <el-alert v-else title="当日计划日量余额为零，不能补排" type="error" :closable="false" show-icon :icon="Warning" />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="原中止记录"><el-input :model-value="`#${target?.id} · ${target?.pond?.name} · 已投喂 ${target?.actualAmountKg} kg`" disabled /></el-form-item>
        <el-form-item label="补排时间（须为原计划当日）"><el-date-picker v-model="rescheduleLocal" type="datetime" value-format="YYYY-MM-DDTHH:mm" /></el-form-item>
        <el-form-item label="补排数量（kg，不超过差额）"><el-input-number v-model="rescheduleForm.plannedAmountKg" :min="0.1" :max="rescheduleBalance.balance" :step="0.5" /></el-form-item>
        <el-form-item label="天气窗口" class="form-span"><el-input v-model="rescheduleForm.weather" placeholder="例：晴朗，微风" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="rescheduleOpen = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="rescheduleBalance.balance <= 0" @click="reschedule">生成补排安排</el-button></template>
    </el-dialog>
    <ConfirmDialog v-model="deleteOpen" :title="target?.rescheduleOfId ? '删除补排安排' : '删除执行安排'" :message="target?.rescheduleOfId ? '删除后解除与原中止记录的关联，原记录保持已中止且可重新补排，确认继续？' : '只能删除尚未开始的执行安排，确认继续？'" danger :loading="saving" @confirm="remove" />
    <PlanDrawer v-model="drawerOpen" :plan="selectedPlan" />
  </div>
</template>

<style scoped>
.relation-tag {
  margin-left: 8px;
}
.aborted-amount {
  color: var(--el-color-warning);
  font-weight: 600;
}
.abort-detail {
  margin-top: 12px;
}
.balance-zero {
  color: var(--el-color-danger);
}
</style>
