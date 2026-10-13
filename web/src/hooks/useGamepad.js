import { useEffect, useRef } from 'react'

// 手柄支持（对标 RomM：RAF 轮询 + 合成键盘事件 + 原生焦点模型）
//
// 设计要点：
//   1. 浏览器原生 Gamepad API 无事件驱动的“持续按键”能力，故用 requestAnimationFrame 轮询
//   2. 手柄 → 合成 KeyboardEvent，复用浏览器原生焦点（Tab/方向键）导航，零第三方依赖
//   3. 面键（A/B/Start）走直接动作事件（gamepad:action），不合成键盘（合成键不触发默认行为）
//   4. 幽灵手柄过滤：Gamepad.index 存在但 connected=false 时跳过
//   5. 提供静音开关（游戏中）与白名单（退出弹窗），避免误触

// PAD_BUTTON 标准映射按钮索引（仅认 mapping === "standard"）
export const PAD_BUTTON = {
  A: 0,
  B: 1,
  X: 2,
  Y: 3,
  LB: 4,
  RB: 5,
  LT: 6,
  RT: 7,
  BACK: 8,
  START: 9,
  L3: 10,
  R3: 11,
  DPAD_UP: 12,
  DPAD_DOWN: 13,
  DPAD_LEFT: 14,
  DPAD_RIGHT: 15,
}

// 死区：摇杆偏移小于该值视为未推动
const STICK_DEADZONE = 0.5
// 方向长按重复：首次触发延迟 / 重复间隔（毫秒）
const REPEAT_DELAY_MS = 350
const REPEAT_INTERVAL_MS = 120

// 方向键索引 → 合成键盘 key
const DIR_TO_KEY = {
  [PAD_BUTTON.DPAD_UP]: 'ArrowUp',
  [PAD_BUTTON.DPAD_DOWN]: 'ArrowDown',
  [PAD_BUTTON.DPAD_LEFT]: 'ArrowLeft',
  [PAD_BUTTON.DPAD_RIGHT]: 'ArrowRight',
}

// 面键索引 → 业务动作名（供视图订阅 gamepad:action）
const BUTTON_TO_ACTION = {
  [PAD_BUTTON.A]: 'confirm',
  [PAD_BUTTON.B]: 'back',
  [PAD_BUTTON.START]: 'menu',
  [PAD_BUTTON.LB]: 'prev-section',
  [PAD_BUTTON.RB]: 'next-section',
}

// 合成键盘事件：向当前焦点元素派发，无焦点时回退到 document
function emitKey(key) {
  const down = new KeyboardEvent('keydown', {
    key,
    code: key,
    bubbles: true,
    cancelable: true,
  })
  const target = document.activeElement || document
  target.dispatchEvent(down)
}

// 合成业务动作事件：视图通过 window.addEventListener('gamepad:action', ...) 订阅
function emitAction(action) {
  window.dispatchEvent(new CustomEvent('gamepad:action', { detail: { action } }))
}

// 计算当前手柄的“方向意图”：D-pad 优先，其次左摇杆
function directionFor(pad) {
  const b = pad.buttons
  if (b[PAD_BUTTON.DPAD_UP]?.pressed) return PAD_BUTTON.DPAD_UP
  if (b[PAD_BUTTON.DPAD_DOWN]?.pressed) return PAD_BUTTON.DPAD_DOWN
  if (b[PAD_BUTTON.DPAD_LEFT]?.pressed) return PAD_BUTTON.DPAD_LEFT
  if (b[PAD_BUTTON.DPAD_RIGHT]?.pressed) return PAD_BUTTON.DPAD_RIGHT

  const ax = pad.axes[0] ?? 0
  const ay = pad.axes[1] ?? 0
  if (ay <= -STICK_DEADZONE) return PAD_BUTTON.DPAD_UP
  if (ay >= STICK_DEADZONE) return PAD_BUTTON.DPAD_DOWN
  if (ax <= -STICK_DEADZONE) return PAD_BUTTON.DPAD_LEFT
  if (ax >= STICK_DEADZONE) return PAD_BUTTON.DPAD_RIGHT
  return -1
}

/**
 * useGamepad 手柄导航 hook
 *
 * @param {object}  opts
 * @param {boolean} opts.enabled  是否启用（游戏中可置 false 全局静音）
 * @param {() => void} opts.onAction  面键业务动作回调（confirm/back/menu/prev-section/next-section）
 * @param {string[]} opts.allowActions  白名单：非空时仅转发其中的动作（用于退出弹窗）
 */
export default function useGamepad({ enabled = true, onAction, allowActions } = {}) {
  // 用 ref 持有最新回调，避免频繁重建 RAF 循环
  const onActionRef = useRef(onAction)
  const allowRef = useRef(allowActions)
  const enabledRef = useRef(enabled)
  onActionRef.current = onAction
  allowRef.current = allowActions
  enabledRef.current = enabled

  useEffect(() => {
    let raf = 0
    // 已按下状态表：按键索引 → true；用于边沿触发（按下瞬间仅一次）
    const pressed = {}
    // 方向长按计时
    let lastDir = -1
    let dirSince = 0
    let dirRepeated = false

    const allowed = (action) => {
      const list = allowRef.current
      if (!list || list.length === 0) return true
      return list.includes(action)
    }

    const tick = (now) => {
      raf = requestAnimationFrame(tick)
      if (!enabledRef.current) {
        // 静音时清空按下状态，避免恢复瞬间误触发
        for (const k in pressed) pressed[k] = false
        lastDir = -1
        return
      }

      const pads = navigator.getGamepads ? navigator.getGamepads() : []
      let pad = null
      for (const p of pads) {
        // 幽灵手柄过滤：仅认 connected 且有按键的标准手柄
        if (p && p.connected && p.mapping === 'standard') {
          pad = p
          break
        }
      }
      if (!pad) {
        lastDir = -1
        return
      }

      // ---- 面键：边沿触发，直接动作 ----
      for (const [idx, action] of Object.entries(BUTTON_TO_ACTION)) {
        const i = Number(idx)
        const isDown = pad.buttons[i]?.pressed
        if (isDown && !pressed[i]) {
          if (allowed(action)) {
            emitAction(action)
            onActionRef.current?.(action)
          }
        }
        pressed[i] = !!isDown
      }

      // ---- 方向：合成键盘事件 + 长按重复 ----
      const dir = directionFor(pad)
      if (dir === -1) {
        lastDir = -1
        dirRepeated = false
        return
      }
      const key = DIR_TO_KEY[dir]
      if (dir !== lastDir) {
        // 新方向：立即触发一次，进入长按计时
        emitKey(key)
        lastDir = dir
        dirSince = now
        dirRepeated = false
      } else {
        const elapsed = now - dirSince
        if (!dirRepeated && elapsed >= REPEAT_DELAY_MS) {
          emitKey(key)
          dirRepeated = true
          dirSince = now
        } else if (dirRepeated && elapsed >= REPEAT_INTERVAL_MS) {
          emitKey(key)
          dirSince = now
        }
      }
    }

    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [])
}
