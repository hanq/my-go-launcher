// 日志级别识别：只认「结构化字段 / 行首前缀」显式声明的级别，
// 避免把正文里的 error/warn 当成级别；未声明的行由调用方决定回退策略。

export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'other'

const LEVEL_TOKENS = [
  'trace',
  'trc',
  'debug',
  'dbg',
  'debu',
  'verbose',
  'info',
  'inf',
  'information',
  'notice',
  'ntc',
  'warn',
  'warning',
  'wrn',
  'error',
  'erro',
  'err',
  'fatal',
  'fata',
  'ftl',
  'panic',
  'pani',
  'pnc',
  'pnk',
  'critical',
  'cri',
  'crit',
  'alert',
  'emerg',
]

const ALIASES: Record<string, LogLevel> = {
  trace: 'debug',
  trc: 'debug',
  debug: 'debug',
  dbg: 'debug',
  debu: 'debug',
  verbose: 'debug',
  info: 'info',
  inf: 'info',
  information: 'info',
  notice: 'info',
  ntc: 'info',
  warn: 'warn',
  warning: 'warn',
  wrn: 'warn',
  error: 'error',
  erro: 'error',
  err: 'error',
  fatal: 'error',
  fata: 'error',
  ftl: 'error',
  panic: 'error',
  pani: 'error',
  pnc: 'error',
  pnk: 'error',
  critical: 'error',
  cri: 'error',
  crit: 'error',
  alert: 'error',
  emerg: 'error',
}

const ALT = LEVEL_TOKENS.join('|')

// 结构化字段：level=info / "level":"ERROR" / severity : warn
// 键前面不允许紧跟字母/数字/下划线/连字符，避免 log_level=debug、log_level 等业务字段被误当级别声明
const STRUCT_RE = new RegExp(
  `(?<![A-Za-z0-9_\\-])["']?(?:level|lvl|severity)["']?\\s*[:=]\\s*["']?(${ALT})\\b`,
  'i',
)
// klog：E1020 / W1020 / I1020
const KLOG_RE = /^([IWEF])\d{4}\s/
// 行首级别：允许最多两段 [方括号] 前缀 + 三类时间戳，覆盖
// [INFO] msg / INFO: msg / 2024-10-20 12:00:00 INFO / [svc] ERROR: x
// [2024-10-20 12:00:00] [WARN] x / Oct 20 12:00:00 host INFO: x / ERRO[0000] x
const HEAD_RE = new RegExp(
  '^\\s*' +
    '(?:\\[[^\\]]{0,48}\\]\\s*){0,2}' +
    '(?:' +
    '\\d{4}[-/.]\\d{1,2}[-/.]\\d{1,2}(?:[T ]\\d{1,2}:\\d{2}(?::\\d{2})?(?:[.,]\\d+)?(?:Z|[+-]\\d{2}:?\\d{2})?)?\\s*' +
    '|(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\\s+\\d{1,2}\\s+\\d{2}:\\d{2}:\\d{2}\\s+(?:\\S+\\s+)?' +
    '|\\d{2}:\\d{2}:\\d{2}(?:[.,]\\d+)?\\s*' +
    ')?' +
    '[\\[\\(|]?\\s*' +
    `(${ALT})` +
    '\\b(?=$|[\\s\\]\\)}:/|,\\-\\[])',
  'i',
)

/** 解析「显式声明」的级别；未声明返回 null */
export function parseLogLevel(line: string): LogLevel | null {
  // 优先级：行首位置声明（klog / 时间戳前缀）> 结构化字段（logrus/slog/JSON）
  // 行首声明更权威，避免正文里的 level=xxx 抢走分类
  let m = KLOG_RE.exec(line)
  if (m) {
    const c = m[1].toUpperCase()
    return c === 'E' || c === 'F' ? 'error' : c === 'W' ? 'warn' : 'info'
  }
  // 只看行首 80 字符，避免正文里的级别词误判
  m = HEAD_RE.exec(line.slice(0, 80))
  if (m) return ALIASES[(m[1] || '').toLowerCase()] ?? null
  m = STRUCT_RE.exec(line)
  if (m) return ALIASES[m[1].toLowerCase()] ?? null
  return null
}

// error/warn 行的后续无级别行（堆栈等）继承其级别
const ANCHOR_LEVELS: LogLevel[] = ['error', 'warn']

/**
 * 入库时确定每行级别：
 * - 显式声明 → 使用声明值；error/warn 设置「续行锚点」
 * - 未声明且锚点生效 → 继承锚点级别（panic/error 堆栈跟随显示）
 * - 空行 → 继承锚点并结束继承
 * - 其余（含启动器系统消息、无级别标记的输出）→ other
 */
export function resolveLevel(
  line: string,
  prevAnchor: LogLevel | null,
  custom?: RegExp | null,
): { level: LogLevel; anchor: LogLevel | null } {
  // 自定义规则优先：捕获组 1（或命名组 level）为级别词，经别名表归一化（INF/ERR/WRN/DBG/FTL/TRC…）
  if (custom) {
    const m = custom.exec(line)
    if (m) {
      const groups = m.groups as Record<string, string> | undefined
      const token = String(groups?.level ?? m[1] ?? m[0] ?? '')
        .toLowerCase()
        .trim()
      const declared = ALIASES[token] ?? null
      if (declared) {
        const anchor = ANCHOR_LEVELS.includes(declared) ? declared : null
        return { level: declared, anchor }
      }
    }
  }
  const declared = parseLogLevel(line)
  if (declared) {
    const anchor = ANCHOR_LEVELS.includes(declared) ? declared : null
    return { level: declared, anchor }
  }
  if (line.trim() === '') {
    return { level: prevAnchor ?? 'other', anchor: null }
  }
  if (prevAnchor) return { level: prevAnchor, anchor: prevAnchor }
  return { level: 'other', anchor: null }
}

/** 编译自定义级别正则；留空返回 { re: null }；语法错误返回 error 提示 */
export function compileLevelPattern(raw: string): { re: RegExp | null; error: string } {
  const s = (raw || '').trim()
  if (!s) return { re: null, error: '' }
  try {
    return { re: new RegExp(s, 'i'), error: '' }
  } catch (e: any) {
    return { re: null, error: String(e?.message ?? e) }
  }
}
