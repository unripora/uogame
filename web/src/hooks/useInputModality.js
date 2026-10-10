import { useEffect } from 'react'

// 输入模态跟踪：记录用户最近使用的输入方式（mouse / touch / key / pad）
// 写入 <html data-input="...">，供 CSS 适配焦点样式
// （手柄模式下焦点环更明显，便于大屏/电视场景）
//
// 关键：只认「可信」事件（isTrusted === true），
//       忽略 useGamepad 合成的 KeyboardEvent（isTrusted 恒为 false），
//       否则手柄导航会被误判为键盘输入，破坏模态判定。
export const INPUT_MODALITY = {
  MOUSE: 'mouse',
  TOUCH: 'touch',
  KEY: 'key',
  PAD: 'pad',
}

function setModality(mode) {
  const root = document.documentElement
  if (root.getAttribute('data-input') !== mode) {
    root.setAttribute('data-input', mode)
  }
}

/**
 * useInputModality 输入模态跟踪 hook
 * @param {object} opts
 * @param {boolean} opts.enabled 是否启用（默认真）
 */
export default function useInputModality({ enabled = true } = {}) {
  useEffect(() => {
    if (!enabled) return

    const onKeyDown = (e) => {
      if (!e.isTrusted) return // 忽略合成键（来自手柄翻译）
      setModality(INPUT_MODALITY.KEY)
    }
    const onMouseDown = () => setModality(INPUT_MODALITY.MOUSE)
    const onTouchStart = () => setModality(INPUT_MODALITY.TOUCH)
    const onPadAction = () => setModality(INPUT_MODALITY.PAD)

    // 手柄动作事件由 useGamepad 派发（真实的物理输入）
    window.addEventListener('keydown', onKeyDown, true)
    window.addEventListener('mousedown', onMouseDown, true)
    window.addEventListener('touchstart', onTouchStart, true)
    window.addEventListener('gamepad:action', onPadAction)

    return () => {
      window.removeEventListener('keydown', onKeyDown, true)
      window.removeEventListener('mousedown', onMouseDown, true)
      window.removeEventListener('touchstart', onTouchStart, true)
      window.removeEventListener('gamepad:action', onPadAction)
    }
  }, [enabled])
}
