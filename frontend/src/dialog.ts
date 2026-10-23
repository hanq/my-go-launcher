import { reactive } from 'vue'

export interface ConfirmOptions {
  title: string
  /** 普通说明文本（保留换行） */
  message?: string
  /** 等宽列表（进程清单等），每项一个条目 */
  lines?: string[]
  /** 危险操作：红色主题 + 警示图标 */
  danger?: boolean
  confirmText?: string
  cancelText?: string
}

interface DialogState {
  visible: boolean
  title: string
  message: string
  lines: string[]
  danger: boolean
  confirmText: string
  cancelText: string
  resolve: ((v: boolean) => void) | null
}

export const dialogState = reactive<DialogState>({
  visible: false,
  title: '',
  message: '',
  lines: [],
  danger: false,
  confirmText: '确认',
  cancelText: '取消',
  resolve: null,
})

/** 主题化确认弹框（替代 window.confirm）；resolve 返回用户是否点击确认 */
export function confirmDialog(opts: ConfirmOptions): Promise<boolean> {
  // 已有未决弹窗时先以「取消」结清，避免 Promise 悬挂
  if (dialogState.resolve) dialogState.resolve(false)
  return new Promise<boolean>((resolve) => {
    dialogState.title = opts.title
    dialogState.message = opts.message ?? ''
    dialogState.lines = opts.lines ?? []
    dialogState.danger = opts.danger ?? false
    dialogState.confirmText = opts.confirmText ?? '确认'
    dialogState.cancelText = opts.cancelText ?? '取消'
    dialogState.resolve = resolve
    dialogState.visible = true
  })
}

/** 关闭弹框并回传结果（由 ConfirmDialog 组件内部调用） */
export function settleDialog(result: boolean): void {
  if (dialogState.resolve) dialogState.resolve(result)
  dialogState.resolve = null
  dialogState.visible = false
}
