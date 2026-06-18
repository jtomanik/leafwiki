import * as importAPI from '@/lib/api/import'
import { ApiError } from '@/lib/api/auth'
import { mapApiError } from '@/lib/api/errors'
import { toast } from 'sonner'
import { create } from 'zustand'
import { useTreeStore } from './tree'

type ImportStore = {
  creatingImportPlan: boolean
  executingImportPlan: boolean
  cancelingImportPlan: boolean
  loadingImportPlan: boolean
  workspaceId: string | null
  operationId: number
  importPlan: importAPI.ImportPlan | null
  importResult: importAPI.ImportResult | null
  createImportPlan: (sourcePath: File, workspaceId: string) => Promise<boolean>
  loadImportPlan: (workspaceId: string) => Promise<void>
  executeImportPlan: (workspaceId: string) => Promise<void>
  cancelImportPlan: (workspaceId: string) => Promise<boolean>
}

const IMPORT_POLL_INTERVAL_MS = 1000
const IMPORT_POLL_RETRY_LIMIT = 3

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

async function pollImportPlanUntilSettled(
  initialPlan: importAPI.ImportPlan,
  workspaceId: string,
  commit: (partial: Partial<ImportStore>) => boolean,
): Promise<importAPI.ImportPlan> {
  let currentPlan = initialPlan
  let consecutivePollErrors = 0

  while (currentPlan.execution_status === 'running') {
    await sleep(IMPORT_POLL_INTERVAL_MS)

    try {
      currentPlan = await importAPI.getImportPlan(workspaceId)
      consecutivePollErrors = 0
      commit({
        importPlan: currentPlan,
        importResult: currentPlan.execution_result ?? null,
      })
    } catch (err) {
      consecutivePollErrors++
      if (consecutivePollErrors >= IMPORT_POLL_RETRY_LIMIT) {
        throw err
      }
    }
  }

  return currentPlan
}

export const useImportStore = create<ImportStore>((set, get) => ({
  importPlan: null,
  workspaceId: null,
  operationId: 0,
  creatingImportPlan: false,
  executingImportPlan: false,
  cancelingImportPlan: false,
  loadingImportPlan: false,
  importResult: null,
  createImportPlan: async (sourcePath: File, workspaceId: string) => {
    const operationId = get().operationId + 1
    const commit = (partial: Partial<ImportStore>) => {
      const state = get()
      if (
        state.workspaceId !== workspaceId ||
        state.operationId !== operationId
      )
        return false
      set(partial)
      return true
    }
    set({
      workspaceId,
      operationId,
      creatingImportPlan: true,
      loadingImportPlan: false,
      executingImportPlan: false,
      cancelingImportPlan: false,
    })
    try {
      const importPlan = await importAPI.createImportPlanFromZip(
        sourcePath,
        workspaceId,
      )
      if (commit({ importPlan, importResult: null })) {
        toast.success('Import plan created successfully')
      }
      return true
    } catch (err) {
      const state = get()
      if (
        state.workspaceId === workspaceId &&
        state.operationId === operationId
      ) {
        toast.error(mapApiError(err, 'Failed to create import plan').message)
      }
      return false
    } finally {
      commit({ creatingImportPlan: false })
    }
  },
  loadImportPlan: async (workspaceId: string) => {
    const operationId = get().operationId + 1
    const commit = (partial: Partial<ImportStore>) => {
      const state = get()
      if (
        state.workspaceId !== workspaceId ||
        state.operationId !== operationId
      )
        return false
      set(partial)
      return true
    }
    set({
      workspaceId,
      operationId,
      loadingImportPlan: true,
      creatingImportPlan: false,
      executingImportPlan: false,
      cancelingImportPlan: false,
      importPlan: null,
      importResult: null,
    })
    try {
      let importPlan = await importAPI.getImportPlan(workspaceId)
      if (
        !commit({
          importPlan,
          importResult: importPlan.execution_result ?? null,
        })
      ) {
        return
      }

      if (importPlan.execution_status === 'running') {
        commit({ executingImportPlan: true })
        importPlan = await pollImportPlanUntilSettled(
          importPlan,
          workspaceId,
          commit,
        )
        commit({
          importPlan,
          importResult: importPlan.execution_result ?? null,
        })
      }
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load import plan')
      if (
        (err instanceof ApiError && err.status === 404) ||
        mapped.code === 'importer_no_plan'
      ) {
        commit({ importPlan: null, importResult: null })
        return
      }
      const state = get()
      if (
        state.workspaceId === workspaceId &&
        state.operationId === operationId
      ) {
        toast.error(mapped.message)
      }
      return
    } finally {
      commit({ loadingImportPlan: false, executingImportPlan: false })
    }
  },
  executeImportPlan: async (workspaceId: string) => {
    const operationId = get().operationId + 1
    const commit = (partial: Partial<ImportStore>) => {
      const state = get()
      if (
        state.workspaceId !== workspaceId ||
        state.operationId !== operationId
      )
        return false
      set(partial)
      return true
    }
    const state = get()
    const importPlan =
      state.workspaceId === workspaceId ? state.importPlan : null
    if (importPlan === null) {
      toast.error('No import plan to execute')
      return
    }
    try {
      set({
        workspaceId,
        operationId,
        executingImportPlan: true,
        creatingImportPlan: false,
        loadingImportPlan: false,
        cancelingImportPlan: false,
        importResult: null,
      })
      let currentPlan = await importAPI.executeImportPlan(workspaceId)
      commit({ importPlan: currentPlan, importResult: null })

      currentPlan = await pollImportPlanUntilSettled(
        currentPlan,
        workspaceId,
        commit,
      )

      if (currentPlan.execution_status === 'completed') {
        if (
          commit({
            importPlan: currentPlan,
            importResult: currentPlan.execution_result ?? null,
          })
        ) {
          toast.success('Import completed successfully')
        }
      } else if (currentPlan.execution_status === 'canceled') {
        if (
          commit({
            importPlan: currentPlan,
            importResult: currentPlan.execution_result ?? null,
          })
        ) {
          toast.success('Import canceled')
        }
      } else if (currentPlan.execution_status === 'failed') {
        commit({ importPlan: currentPlan })
        throw new Error(
          currentPlan.execution_error || 'Import execution failed',
        )
      }
    } catch (err) {
      const state = get()
      if (
        state.workspaceId === workspaceId &&
        state.operationId === operationId
      ) {
        toast.error(mapApiError(err, 'Failed to execute import plan').message)
      }
    } finally {
      commit({ executingImportPlan: false })
      // reload tree
      void useTreeStore.getState().reloadTree(workspaceId)
    }
  },
  cancelImportPlan: async (workspaceId: string) => {
    const operationId = get().operationId + 1
    const commit = (partial: Partial<ImportStore>) => {
      const state = get()
      if (
        state.workspaceId !== workspaceId ||
        state.operationId !== operationId
      )
        return false
      set(partial)
      return true
    }
    const state = get()
    const importPlan =
      state.workspaceId === workspaceId ? state.importPlan : null
    if (importPlan === null) {
      toast.error('No import plan to clear')
      return false
    }
    try {
      set({
        workspaceId,
        operationId,
        cancelingImportPlan: true,
        creatingImportPlan: false,
        loadingImportPlan: false,
        executingImportPlan: false,
      })
      const response = await importAPI.cancelImportPlan(workspaceId)

      if (
        response &&
        response.execution_status === 'running' &&
        response.cancel_requested
      ) {
        commit({ importPlan: response })
        const finalPlan = await pollImportPlanUntilSettled(
          response,
          workspaceId,
          commit,
        )
        if (
          commit({
            importPlan: finalPlan,
            importResult: finalPlan.execution_result ?? null,
          })
        ) {
          toast.success(
            finalPlan.execution_status === 'canceled'
              ? 'Import canceled'
              : 'Import finished before cancellation completed',
          )
        }
        void useTreeStore.getState().reloadTree(workspaceId)
        return finalPlan.execution_status === 'canceled'
      }

      if (commit({ importPlan: null, importResult: null })) {
        toast.success('Import plan cleared')
      }
      return true
    } catch (err) {
      const state = get()
      if (
        state.workspaceId === workspaceId &&
        state.operationId === operationId
      ) {
        toast.error(
          mapApiError(err, 'Failed to cancel or clear import plan').message,
        )
      }
      return false
    } finally {
      commit({ cancelingImportPlan: false })
    }
  },
}))
