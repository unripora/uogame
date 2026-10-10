// 游戏类型定义与扩展名映射（从产物反解 Ri() 逻辑 + 类型下拉文案）
// value 与后端 game_type 字段一致

export const GAME_TYPES = [
  { value: 'H5', label: 'H5 网页小游戏 (标准单屏/模拟手机框)', kind: 'h5' },
  { value: 'H5_FULL', label: 'H5 全屏网页 (整页自适应/支持滚动/无外框)', kind: 'h5' },
  { value: 'FLASH', label: 'Flash 游戏 (Adobe Flash SWF)', kind: 'flash' },
  { value: 'FC', label: 'FC (任天堂 NES)', kind: 'rom' },
  { value: 'SNES', label: 'SNES/SFC (超级任天堂)', kind: 'rom' },
  { value: 'N64', label: 'N64 (任天堂64)', kind: 'rom' },
  { value: 'NDS', label: 'NDS (任天堂 DS)', kind: 'rom' },
  { value: 'GBA', label: 'GBA (Game Boy Advance)', kind: 'rom' },
  { value: 'GB', label: 'GB/GBC (Game Boy)', kind: 'rom' },
  { value: 'MD', label: 'MD (世嘉 Mega Drive)', kind: 'rom' },
  { value: 'SEGACD', label: 'Sega CD (世嘉光驱扩展)', kind: 'rom' },
  { value: 'SEGA32X', label: 'Sega 32X (世嘉32位卡带)', kind: 'rom' },
  { value: 'GG', label: 'Sega GG (世嘉掌机 Game Gear)', kind: 'rom' },
  { value: 'SMS', label: 'Sega MS (世嘉 Master System)', kind: 'rom' },
  { value: 'SATURN', label: 'Sega Saturn (世嘉土星)', kind: 'rom' },
  { value: 'ARCADE', label: 'Arcade (街机 - FBNeo)', kind: 'rom' },
  { value: 'MAME', label: 'MAME (街机 - Mame2003)', kind: 'rom' },
  { value: 'A2600', label: 'Atari 2600 (雅达利)', kind: 'rom' },
  { value: 'A5200', label: 'Atari 5200 (雅达利)', kind: 'rom' },
  { value: 'A7800', label: 'Atari 7800 (雅达利)', kind: 'rom' },
  { value: 'LYNX', label: 'Atari Lynx (雅达利掌机)', kind: 'rom' },
  { value: 'JAGUAR', label: 'Jaguar (雅达利美洲豹)', kind: 'rom' },
  { value: 'PCFX', label: 'PC-FX (NEC 光盘游戏机)', kind: 'rom' },
  { value: '3DO', label: '3DO (三洋/松下3DO)', kind: 'rom' },
  { value: 'WS', label: 'WS (万代 Wonderswan)', kind: 'rom' },
  { value: 'PCE', label: 'PCE (PC Engine)', kind: 'rom' },
  { value: 'VB', label: 'VB (Virtual Boy)', kind: 'rom' },
  { value: 'NGP', label: 'NGP (Neo Geo Pocket)', kind: 'rom' },
  { value: 'COLECO', label: 'ColecoVision (科莱科游戏机)', kind: 'rom' },
  { value: 'PSX', label: 'PS1 (索尼 PlayStation)', kind: 'rom' },
]

// 源文件导入：扩展名 → game_type 值（与后端 sourceGameTypeByCode / emulator coreMapping 对齐）
const EXT_GAME_TYPE = {
  '.nes': 'FC', '.fds': 'FC', '.unf': 'FC',
  '.gb': 'GB', '.gbc': 'GB',
  '.gba': 'GBA',
  '.sfc': 'SNES', '.smc': 'SNES',
  '.md': 'MD', '.gen': 'MD', '.smd': 'MD', '.bin': 'MD',
  '.32x': 'SEGA32X',
  '.sms': 'SMS', '.gg': 'GG',
  '.pce': 'PCE',
  '.ngp': 'NGP', '.ngc': 'NGP',
  '.ws': 'WS', '.wsc': 'WS',
  '.vb': 'VB',
  '.n64': 'N64', '.z64': 'N64', '.v64': 'N64',
  '.nds': 'NDS',
  '.cue': 'PSX', '.iso': 'PSX', '.chd': 'PSX', '.pbp': 'PSX',
  '.a26': 'A2600', '.a52': 'A5200', '.a78': 'A7800',
  '.col': 'COLECO',
  '.swf': 'FLASH',
  '.html': 'H5', '.htm': 'H5',
}

// 源文件导入允许选择的扩展名（file input accept）
export const SOURCE_ACCEPT = Object.keys(EXT_GAME_TYPE).join(',')

// 依据文件名扩展名推断 game_type（未识别返回空串）
export function gameTypeByExt(name) {
  const m = String(name || '').toLowerCase().match(/\.[^.]+$/)
  return m ? EXT_GAME_TYPE[m[0]] || '' : ''
}

// 每种类型允许的扩展名（产物 Ri() 逻辑）
export function allowedExtensions(type) {
  switch (String(type).toUpperCase()) {
    case 'GBA':
      return ['.gba', '.zip']
    case 'FLASH':
      return ['.swf', '.zip']
    case 'FC':
      return ['.nes', '.fc', '.zip']
    case 'SNES':
      return ['.sfc', '.smc', '.zip']
    case 'GB':
      return ['.gb', '.zip']
    case 'GBC':
      return ['.gbc', '.zip']
    case 'MD':
      return ['.md', '.gen', '.smd', '.zip']
    case 'N64':
      return ['.n64', '.z64', '.zip']
    case 'NDS':
      return ['.nds', '.zip']
    case 'ARCADE':
    case 'DOS':
    case 'MAME':
      return ['.zip']
    case 'PSX':
    case 'SATURN':
    case 'SEGACD':
      return ['.bin', '.cue', '.img', '.m3u', '.iso', '.chd', '.pbp', '.zip']
    case 'SEGA32X':
      return ['.32x', '.bin', '.zip']
    case 'PCE':
      return ['.pce', '.bin', '.cue', '.zip']
    case 'VB':
      return ['.vb', '.bin', '.zip']
    case '3DO':
      return ['.iso', '.bin', '.cue', '.chd', '.zip']
    case 'A5200':
      return ['.a52', '.bin', '.zip']
    case 'COLECO':
      return ['.col', '.bin', '.zip']
    case 'PCFX':
      return ['.bin', '.cue', '.img', '.iso', '.zip']
    case 'JAGUAR':
      return ['.j64', '.jag', '.bin', '.zip']
    case 'WS':
      return ['.ws', '.wsc', '.zip']
    case 'NGP':
      return ['.ngp', '.ngc', '.zip']
    case 'GG':
      return ['.gg', '.zip']
    case 'SMS':
      return ['.sms', '.zip']
    case 'A2600':
      return ['.a26', '.zip']
    case 'A7800':
      return ['.a78', '.zip']
    case 'LYNX':
      return ['.lnx', '.zip']
    default:
      return ['.zip']
  }
}

// 根据 game_type 判断走哪种游玩方式
export function playKind(type) {
  const t = String(type || 'H5').toUpperCase()
  if (t === 'FLASH') return 'flash'
  if (t === 'H5' || t === 'H5_FULL' || t === 'HTML5') return 'h5'
  return 'rom'
}

export function gameTypeLabel(type) {
  const found = GAME_TYPES.find((g) => g.value === String(type).toUpperCase())
  return found ? found.label : String(type || 'H5')
}

// 平台标签配色：按标签文本稳定映射到一组预设色（同一标签颜色固定）
const PLATFORM_TAG_COLORS = [
  'bg-emerald-500/90',
  'bg-sky-500/90',
  'bg-violet-500/90',
  'bg-amber-500/90',
  'bg-rose-500/90',
  'bg-teal-500/90',
  'bg-indigo-500/90',
  'bg-orange-500/90',
]

export function platformTagColor(tag) {
  const s = String(tag || '')
  let h = 0
  for (let i = 0; i < s.length; i += 1) {
    h = (h * 31 + s.charCodeAt(i)) >>> 0
  }
  return PLATFORM_TAG_COLORS[h % PLATFORM_TAG_COLORS.length]
}
