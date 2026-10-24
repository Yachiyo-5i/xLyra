import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import celebratingUrl from '@/assets/clawd/clawd-celebrating.svg'
import clappingUrl from '@/assets/clawd/clawd-clapping.svg'
import coffeeUrl from '@/assets/clawd/clawd-coffee.svg'
import confusedUrl from '@/assets/clawd/clawd-confused.svg'
import coolUrl from '@/assets/clawd/clawd-cool.svg'
import crabWalkingUrl from '@/assets/clawd/clawd-crab-walking.svg'
import dancingUrl from '@/assets/clawd/clawd-dancing.svg'
import flexingUrl from '@/assets/clawd/clawd-flexing.svg'
import jugglingUrl from '@/assets/clawd/clawd-juggling.svg'
import loveUrl from '@/assets/clawd/clawd-love.svg'
import meditatingUrl from '@/assets/clawd/clawd-meditating.svg'
import musicUrl from '@/assets/clawd/clawd-music.svg'
import readingUrl from '@/assets/clawd/clawd-reading.svg'
import singingUrl from '@/assets/clawd/clawd-singing.svg'
import surprisedUrl from '@/assets/clawd/clawd-surprised.svg'
import yawningUrl from '@/assets/clawd/clawd-yawning.svg'
import idleUrl from '@/assets/clawd/clawd-idle.svg'
import sleepingUrl from '@/assets/clawd/clawd-sleeping.svg'
import staticUrl from '@/assets/clawd/clawd-static.svg'
import thinkingUrl from '@/assets/clawd/clawd-thinking.svg'
import typingUrl from '@/assets/clawd/clawd-typing.svg'
import wavingUrl from '@/assets/clawd/clawd-waving.svg'
import { cn } from '@/lib/utils'

/**
 * 输入框上方的小螃蟹（Clawd）。素材来自 clawd-pet（MIT，见 assets/clawd/NOTICE.md），
 * 纯 CSS 动画的 SVG，用 <img> 引入即可播放。
 *
 * 状态优先级：思考（生成中）> 庆祝（刚生成完）> 挥手（被指到 / 点击）> 打字 > 睡着 > 待机。
 */
export type ClawdMood = 'idle' | 'typing' | 'thinking' | 'celebrating' | 'waving' | 'sleeping'

const MOOD_SRC: Record<ClawdMood, string> = {
  idle: idleUrl,
  typing: typingUrl,
  thinking: thinkingUrl,
  celebrating: celebratingUrl,
  waving: wavingUrl,
  sleeping: sleepingUrl,
}

/** 待机时随机冒出来的小动作，每个播放 4–6 秒后回到呼吸待机 */
const IDLE_ACTIONS = [
  { src: dancingUrl, ms: 5000 },
  { src: loveUrl, ms: 4500 },
  { src: coolUrl, ms: 4500 },
  { src: yawningUrl, ms: 5000 },
  { src: coffeeUrl, ms: 6000 },
  { src: readingUrl, ms: 6000 },
  { src: musicUrl, ms: 5500 },
  { src: meditatingUrl, ms: 6000 },
  { src: clappingUrl, ms: 4000 },
  { src: flexingUrl, ms: 4500 },
  { src: singingUrl, ms: 5500 },
  { src: confusedUrl, ms: 4500 },
  { src: surprisedUrl, ms: 3500 },
  { src: jugglingUrl, ms: 6000 },
] as const

/**
 * 待机节奏：做完一个动作后先老老实实待着一阵（呼吸、东张西望），再做下一个；
 * 有 30% 的概率额外多歇一会儿，看起来像真的宠物，而不是一直在表演。
 */
const IDLE_REST_MIN_MS = 14_000
const IDLE_REST_MAX_MS = 30_000
const EXTRA_REST_CHANCE = 0.3
const EXTRA_REST_MIN_MS = 15_000
const EXTRA_REST_MAX_MS = 35_000
/** 在「家」（最右侧）时有这个概率走出去；一旦离家，下一个动作一定是往回走（走回家或先走回一部分） */
const WALK_CHANCE_AT_HOME = 0.3
const WALK_CHANCE_AWAY = 1
/** 横着走的速度（px/s）与从家出发的最短 / 最长距离 */
const WALK_SPEED = 46
const WALK_OUT_MIN = 100
const WALK_OUT_MAX = 280
/** 离家超过这个距离才算「在外面」 */
const AWAY_THRESHOLD = 40
/** 离家后，直接走回家的概率；否则先往回走一部分（保证每次都在靠近家） */
const GO_HOME_CHANCE = 0.65
/** 螃蟹本体占画布宽度的比例（居中），用来把「螃蟹站到边缘」换算成画布的 right 值 */
const BODY_WIDTH_RATIO = 0.33
/** 螃蟹本体右缘 / 左缘离输入框两端的距离（桌面 / 手机） */
const EDGE_GAP_DESKTOP = 18
const EDGE_GAP_MOBILE = 22

const TYPING_HOLD_MS = 1500
const CELEBRATE_MS = 3200
const WAVE_MS = 2200
const SLEEP_AFTER_MS = 90_000

function usePrefersReducedMotion() {
  const [reduced, setReduced] = useState(() =>
    typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  )
  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const onChange = () => setReduced(query.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  return reduced
}

/**
 * 由输入框的状态推出螃蟹的心情：value 变化视为在打字，streaming 为生成中，结束后庆祝一下，久不操作就睡着。
 * 随 props 变化的状态在渲染期调整（React 推荐写法），只有「过一会儿恢复」这类定时器放在 effect 里。
 */
function useClawdMood({ value, streaming }: { value: string; streaming: boolean }) {
  const [typing, setTyping] = useState(false)
  const [celebrating, setCelebrating] = useState(false)
  const [waving, setWaving] = useState(false)
  const [sleeping, setSleeping] = useState(false)
  // 每次有操作（打字、生成状态变化、指向螃蟹）加一，用来重置「睡着」倒计时和延长挥手
  const [activity, setActivity] = useState(0)
  const [prev, setPrev] = useState({ value, streaming })

  if (prev.value !== value || prev.streaming !== streaming) {
    setPrev({ value, streaming })
    setActivity((n) => n + 1)
    setSleeping(false)
    if (prev.value !== value) setTyping(Boolean(value))
    if (prev.streaming && !streaming) setCelebrating(true)
  }

  useEffect(() => {
    if (!typing) return undefined
    const timer = window.setTimeout(() => setTyping(false), TYPING_HOLD_MS)
    return () => window.clearTimeout(timer)
  }, [typing, value])

  useEffect(() => {
    if (!celebrating) return undefined
    const timer = window.setTimeout(() => setCelebrating(false), CELEBRATE_MS)
    return () => window.clearTimeout(timer)
  }, [celebrating])

  useEffect(() => {
    if (!waving) return undefined
    const timer = window.setTimeout(() => setWaving(false), WAVE_MS)
    return () => window.clearTimeout(timer)
  }, [waving, activity])

  useEffect(() => {
    const timer = window.setTimeout(() => setSleeping(true), SLEEP_AFTER_MS)
    return () => window.clearTimeout(timer)
  }, [activity])

  const wave = useCallback(() => {
    setActivity((n) => n + 1)
    setSleeping(false)
    setWaving(true)
  }, [])

  const mood: ClawdMood = streaming
    ? 'thinking'
    : celebrating
      ? 'celebrating'
      : waving
        ? 'waving'
        : typing
          ? 'typing'
          : sleeping
            ? 'sleeping'
            : 'idle'

  return { mood, wave }
}

type ClawdPetProps = {
  value: string
  streaming: boolean
  className?: string
}

/** 画布 right 值：让螃蟹本体几乎站到输入框最右侧；画布本身会伸出输入框一点，但它是透明且不挡操作的 */
function homeRight(canvasSize: number, edgeGap: number) {
  return Math.round(edgeGap - BODY_WIDTH_RATIO * canvasSize)
}

function randomBetween(min: number, max: number) {
  return min + Math.random() * (max - min)
}

/**
 * 待机时的小动作：隔几秒随机播一个小动作，或沿输入框顶边横着走一段。
 * 只在 enabled（基础心情是待机且没开减少动态效果）时调度；一旦被打字、生成等打断就立即清掉。
 */
function useIdleBehavior({ enabled, containerRef, initialRight, edgeGap }: {
  enabled: boolean
  containerRef: RefObject<HTMLDivElement | null>
  initialRight: number
  edgeGap: number
}) {
  const [action, setAction] = useState<string | null>(null)
  const [walking, setWalking] = useState(false)
  const [right, setRight] = useState(initialRight)
  const [walkMs, setWalkMs] = useState(0)
  const rightRef = useRef(initialRight)
  const lastActionRef = useRef<string | null>(null)

  useEffect(() => {
    if (!enabled) return undefined
    let timer: number | undefined

    // 做完一个动作后先歇一阵；偶尔多歇一会儿
    const rest = () => {
      let delay = randomBetween(IDLE_REST_MIN_MS, IDLE_REST_MAX_MS)
      if (Math.random() < EXTRA_REST_CHANCE) delay += randomBetween(EXTRA_REST_MIN_MS, EXTRA_REST_MAX_MS)
      timer = window.setTimeout(run, delay)
    }

    const homeOf = (size: number) => homeRight(size, edgeGap)

    const walk = () => {
      const container = containerRef.current
      const parent = container?.parentElement
      if (!container || !parent) return false
      const size = container.offsetWidth
      const home = homeOf(size)
      // 画布往左走的上限：螃蟹本体左缘不越过输入框左端
      const farthest = parent.clientWidth - size + BODY_WIDTH_RATIO * size - edgeGap
      const range = farthest - home
      if (range < WALK_OUT_MIN) return false

      const current = rightRef.current
      const away = current - home
      let target: number
      if (away > AWAY_THRESHOLD) {
        // 在外面：往回走，多数直接回家，否则先走回一部分
        target = Math.random() < GO_HOME_CHANCE ? home : home + away * randomBetween(0.2, 0.6)
      } else {
        // 在家：向左走出去一段
        target = home + randomBetween(Math.min(WALK_OUT_MIN, range), Math.min(WALK_OUT_MAX, range))
      }
      target = Math.min(farthest, Math.max(home, target))
      const distance = Math.abs(target - current)
      if (distance < 20) return false

      const duration = (distance / WALK_SPEED) * 1000
      rightRef.current = target
      setWalking(true)
      setWalkMs(duration)
      setRight(target)
      timer = window.setTimeout(() => {
        setWalking(false)
        rest()
      }, duration)
      return true
    }

    const play = () => {
      const pool = IDLE_ACTIONS.filter((item) => item.src !== lastActionRef.current)
      const next = pool[Math.floor(Math.random() * pool.length)]
      lastActionRef.current = next.src
      setAction(next.src)
      timer = window.setTimeout(() => {
        setAction(null)
        rest()
      }, next.ms)
    }

    function run() {
      const container = containerRef.current
      const away = container ? rightRef.current - homeOf(container.offsetWidth) > AWAY_THRESHOLD : false
      if (Math.random() < (away ? WALK_CHANCE_AWAY : WALK_CHANCE_AT_HOME) && walk()) return
      play()
    }

    rest()
    return () => {
      window.clearTimeout(timer)
      setAction(null)
      setWalking(false)
    }
  }, [enabled, containerRef, edgeGap])

  return { action, walking, right, walkMs }
}

/**
 * 趴在输入框顶边的螃蟹：父级需要是 relative，它自己贴着父级顶边向上排，默认在右侧，待机时会沿顶边来回走动。
 * 素材的画布是 45×45 的 viewBox，螃蟹脚底在画布约 90% 的位置，所以整体下沉 10% 让脚踩在边上。
 */
export function ClawdPet({ value, streaming, className }: ClawdPetProps) {
  const { mood, wave } = useClawdMood({ value, streaming })
  const reduced = usePrefersReducedMotion()
  const containerRef = useRef<HTMLDivElement>(null)
  // 桌面画布 168、手机 120（与下面的 size 类保持一致）
  const [layout] = useState(() => {
    const desktop = typeof window !== 'undefined' && window.matchMedia('(min-width: 768px)').matches
    const edgeGap = desktop ? EDGE_GAP_DESKTOP : EDGE_GAP_MOBILE
    return { edgeGap, initialRight: homeRight(desktop ? 168 : 120, edgeGap) }
  })
  const idle = useIdleBehavior({
    enabled: mood === 'idle' && !reduced,
    containerRef,
    initialRight: layout.initialRight,
    edgeGap: layout.edgeGap,
  })

  // 待机时：走路 > 小动作 > 呼吸待机；其他心情优先
  const src = reduced
    ? staticUrl
    : mood !== 'idle'
      ? MOOD_SRC[mood]
      : idle.walking
        ? crabWalkingUrl
        : (idle.action ?? MOOD_SRC.idle)

  return (
    <div
      ref={containerRef}
      aria-hidden="true"
      style={{
        right: idle.right,
        transition: idle.walking ? `right ${idle.walkMs}ms linear` : undefined,
      }}
      className={cn(
        'pointer-events-none absolute bottom-full z-10 size-[120px] translate-y-[10%] select-none md:size-[168px]',
        className,
      )}
    >
      <img
        // 换状态时用 key 重建 <img>，让新动画从头播放
        key={src}
        src={src}
        alt=""
        draggable={false}
        className="pointer-events-none size-full"
      />
      {/* 只有螃蟹身体这一小块响应指向 / 点击，画布其余透明区域不挡住下面的内容 */}
      <div onPointerEnter={wave} onClick={wave} className="pointer-events-auto absolute inset-x-[26%] bottom-[8%] h-[38%] cursor-pointer" />
    </div>
  )
}
