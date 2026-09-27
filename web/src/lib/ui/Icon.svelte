<script module lang="ts">
  import {
    Activity, ArrowDown, ArrowDownUp, ArrowLeft, ArrowLeftRight, ArrowRight, ArrowUp, ArrowUpRight,
    AudioLines, Ban, Bot, Brackets, Brain, BrainCircuit, Bug, Cable, ChartColumn, Check, ChevronDown,
    ChevronLeft, ChevronRight, ChevronUp, ChevronsUpDown, CircleAlert, CircleArrowUp, CircleCheck,
    CircleDollarSign, CircleDot, CircleQuestionMark, CircleStop, CircleUser, CircleX, Clapperboard,
    Clock, Cloud, CloudUpload, Coins, Copy, CopyCheck, Crosshair, Download, Drama, Ellipsis,
    EllipsisVertical, ExternalLink, Eye, EyeOff, FileText, Flag, FlaskConical, Funnel, Gauge, Globe,
    GripVertical, HandHeart, History, Hourglass, Image, Images, Inbox, Info, Key, KeyRound,
    LayoutDashboard, LayoutGrid, Layers, Link, List, ListChecks, LoaderCircle, Lock, LockKeyhole,
    LockOpen, LogIn, LogOut, Menu, Mic, Minus, Moon, Network, PanelLeftClose, PanelLeftOpen, PanelRightOpen, Pencil, Play, Plus,
    Power, PowerOff, Puzzle, Radio, RadioTower, RefreshCw, Rocket, RotateCcw, Route, Save, Scan,
    Scissors, ScrollText, Search, Send, Server, Settings2, Shield, ShieldAlert, ShieldCheck,
    SlidersHorizontal, Sparkles, Speech, SquareTerminal, Star, Sun, Terminal, Timer, ToggleLeft,
    ToggleRight, Trash2, TriangleAlert, Unlink, Upload, Waypoints, Wifi, X, Zap,
  } from 'lucide-svelte'

  /**
   * One vocabulary for the whole app. Keys include the legacy Material Symbols
   * names still passed as strings (Button `icon=`, nav model), so call sites can
   * migrate without renaming.
   */
  export const ICONS = {
    // Navigation
    overview: LayoutDashboard, endpoint: Radio, providers: Server, combos: Waypoints, proxy: Network,
    usage: Activity, quota: Gauge, console: ScrollText, 'token-saver': Scissors, flags: Flag,
    embedding: Brackets, image: Image, tts: Speech, stt: Mic, video: Clapperboard, 'system-one': BrainCircuit,
    web: Globe, cli: Terminal, skills: Puzzle, personas: Drama, bounty: Crosshair, settings: Settings2,
    media: Images,

    // Actions & status
    add: Plus, close: X, check: Check, edit: Pencil, delete: Trash2, copy: Copy, copied: CopyCheck,
    search: Search, filter: Funnel, refresh: RefreshCw, reset: RotateCcw, save: Save, send: Send,
    upload: Upload, download: Download, external: ExternalLink, link: Link, unlink: Unlink,
    play: Play, stop: CircleStop, power: Power, 'power-off': PowerOff, login: LogIn, logout: LogOut,
    menu: Menu, more: Ellipsis, 'more-vertical': EllipsisVertical, drag: GripVertical, sort: ChevronsUpDown,
    spinner: LoaderCircle, info: Info, warning: TriangleAlert, error: CircleAlert, success: CircleCheck,
    cancel: CircleX, blocked: Ban, help: CircleQuestionMark, lock: Lock, unlock: LockOpen, key: Key,
    shield: Shield, 'shield-ok': ShieldCheck, 'shield-alert': ShieldAlert, eye: Eye, 'eye-off': EyeOff,
    clock: Clock, timer: Timer, history: History, sun: Sun, moon: Moon, user: CircleUser, star: Star,
    heart: HandHeart, rocket: Rocket, bot: Bot, brain: Brain, cost: CircleDollarSign, tokens: Coins,
    requests: ArrowDownUp, bolt: Zap, tunnel: Cloud, tailscale: Wifi, route: Route, layers: Layers,
    grid: LayoutGrid, list: List, doc: FileText, lab: FlaskConical, scan: Scan,
    sliders: SlidersHorizontal, sparkles: Sparkles, panel: PanelRightOpen, 'sidebar-close': PanelLeftClose, 'sidebar-open': PanelLeftOpen, inbox: Inbox, dot: CircleDot,
    audio: AudioLines, chart: ChartColumn, cable: Cable, broadcast: RadioTower, hourglass: Hourglass,
    'arrow-left': ArrowLeft, 'arrow-right': ArrowRight, 'arrow-up': ArrowUp, 'arrow-down': ArrowDown,
    'arrow-out': ArrowUpRight, swap: ArrowLeftRight, 'chevron-down': ChevronDown, 'chevron-up': ChevronUp,
    'chevron-left': ChevronLeft, 'chevron-right': ChevronRight, upgrade: CircleArrowUp,

    // Legacy Material Symbols names
    api: Radio, dns: Server, lan: Network, bar_chart: ChartColumn, data_usage: Gauge, terminal: SquareTerminal,
    savings: Scissors, toggle_on: ToggleRight, toggle_off: ToggleLeft, data_array: Brackets, brush: Image,
    record_voice_over: Speech, mic: Mic, movie: Clapperboard, psychology: Drama, neurology: BrainCircuit,
    travel_explore: Globe, extension: Puzzle, bug_report: Crosshair, perm_media: Images,
    receipt_long: ScrollText, monitoring: Activity, swap_vert: ArrowDownUp, toll: Coins,
    payments: CircleDollarSign, space_dashboard: LayoutDashboard, content_copy: Copy, open_in_new: ExternalLink,
    progress_activity: LoaderCircle, check_circle: CircleCheck, expand_more: ChevronDown,
    keyboard_arrow_down: ChevronDown, keyboard_arrow_up: ChevronUp, chevron_right: ChevronRight,
    chevron_left: ChevronLeft, arrow_back: ArrowLeft, arrow_forward: ArrowRight, arrow_upward: ArrowUp,
    arrow_outward: ArrowUpRight, vpn_key: KeyRound, visibility: Eye, visibility_off: EyeOff,
    rocket_launch: Rocket, smart_toy: Bot, lock_open: LockOpen, lock_clock: LockKeyhole, link_off: Unlink,
    hourglass_top: Hourglass, sync: RefreshCw, sync_alt: ArrowLeftRight, restart_alt: RotateCcw,
    remove: Minus, power_off: PowerOff, play_arrow: Play, language: Globe, graphic_eq: AudioLines,
    crop_free: Scan, cloud: Cloud, cloud_upload: CloudUpload, checklist: ListChecks, block: Ban,
    alt_route: Route, account_circle: CircleUser, apps: LayoutGrid, science: FlaskConical,
    wifi_tethering: RadioTower, light_mode: Sun, dark_mode: Moon, volunteer_activism: HandHeart,
    autorenew: RefreshCw, radio_button_checked: CircleDot, upgrade_alt: CircleArrowUp,
  } as const

  export type IconName = keyof typeof ICONS
</script>

<script lang="ts">
  let {
    name,
    icon,
    size = 18,
    label,
    spin = false,
    class: klass = '',
  }: {
    name?: IconName | (string & {})
    /** A lucide-svelte component, for icons outside the shared vocabulary. */
    icon?: typeof X
    size?: number
    /** Accessible name; omit for decorative icons. */
    label?: string
    spin?: boolean
    class?: string
  } = $props()

  const Glyph = $derived.by(() => {
    if (icon) return icon
    const found = name ? (ICONS as Record<string, typeof X>)[name] : undefined
    if (!found && name && import.meta.env.DEV) console.warn(`[Icon] no mapping for "${name}"`)
    return found ?? CircleQuestionMark
  })
</script>

<Glyph
  {size}
  strokeWidth={1.5}
  stroke-linecap="square"
  stroke-linejoin="miter"
  class="shrink-0 {spin ? 'animate-spin' : ''} {klass}"
  aria-label={label}
  role={label ? 'img' : undefined}
/>
