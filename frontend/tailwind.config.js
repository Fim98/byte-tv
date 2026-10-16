/** @type {import('tailwindcss').Config} */
// Byte TV 设计令牌 —— 直接消费 open-claude-ui 的 Claude Design System (CDS)。
// 令牌来源：code-examples/cds-theme.css + designs/byte-tv/index.html
// 主题策略：class 策略，<html data-theme="dark|light">，与 lib/theme.js 对齐。
export default {
  content: ["./index.html", "./src/**/*.{js,jsx}"],
  darkMode: ["class", '[data-theme="dark"]'],
  theme: {
    extend: {
      colors: {
        // ---- 表面阶梯（微亮度对比分层，不用纯黑纯白）----
        surface: {
          0: "var(--cds-surface-0)",
          1: "var(--cds-surface-1)",
          2: "var(--cds-surface-2)",
          3: "var(--cds-surface-3)",
          popover: "var(--cds-surface-popover)",
        },
        // ---- 品牌陶土色阶 ----
        clay: {
          DEFAULT: "var(--cds-clay)",
          hover: "var(--cds-clay-hover)",
          light: "var(--cds-clay-light)",
        },
        // ---- 文本色阶 ----
        ink: {
          DEFAULT: "var(--cds-text-primary)",
          secondary: "var(--cds-text-secondary)",
          muted: "var(--cds-text-muted)",
        },
        // ---- 边框 / 分隔 ----
        hairline: "var(--hairline)",
        rail: "var(--rail)",
        subtle: "var(--cds-border)",
        strong: "var(--cds-border-strong)",
        // ---- 语义色 ----
        danger: "var(--cds-fill-danger)",
        ok: "var(--cds-ok)",
        warn: "var(--cds-warn)",
      },
      borderRadius: {
        xs: "6px",
        sm: "8px",
        card: "10px",
        lg: "12px",
        dialog: "16px",
        bubble: "18px",
        composer: "20px",
        pill: "9999px",
      },
      boxShadow: {
        panel: "var(--cds-panel-shadow)",
        "panel-hover": "var(--cds-panel-shadow-hover)",
        popover: "var(--cds-shadow-popover)",
        sm: "var(--cds-shadow-sm)",
        md: "var(--cds-shadow-md)",
        lg: "var(--cds-shadow-lg)",
        focus: "var(--cds-focus-shadow)",
        composer: "var(--cds-shadow-composer)",
        film: "0 0 0 1px var(--cds-border-strong), 0 30px 70px -22px var(--film-shadow)",
      },
      fontFamily: {
        sans: ['"Noto Sans SC"', "-apple-system", "BlinkMacSystemFont", '"PingFang SC"', "system-ui", "sans-serif"],
        mono: ['"JetBrains Mono"', "ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
      transitionTimingFunction: {
        // 对齐 CDS 的有机动效曲线，禁用通用 linear
        out: "cubic-bezier(.16, 1, .3, 1)",
        snappy: "cubic-bezier(.2, .9, .24, 1)",
      },
      // 布局常量（与 tokens.css 的 --side-w / --top-h 对齐）
      width: { side: "228px" },
      spacing: { top: "52px", page: "26px", side: "228px" },
      keyframes: {
        // 陶土呼吸指示灯（2.4s 周期）
        breathe: {
          "0%, 100%": { filter: "brightness(.96)", transform: "scale(.92)" },
          "50%": { filter: "brightness(1.1)", transform: "scale(1.08)" },
        },
        // 活动脉冲灯
        pulseDot: {
          "0%, 100%": { opacity: ".3", transform: "scale(.85)" },
          "50%": { opacity: "1", transform: "scale(1.15)" },
        },
        // 三点波浪
        wave: {
          "0%, 8%": { opacity: ".2", transform: "translateY(2px)" },
          "22%": { opacity: "1", transform: "translateY(-2.5px)" },
          "36%, 70%": { opacity: "1", transform: "translateY(0)" },
          "90%, 100%": { opacity: ".2", transform: "translateY(0)" },
        },
        // 骨架屏扫光
        sweep: {
          "0%": { transform: "translateX(-100%)" },
          "100%": { transform: "translateX(100%)" },
        },
        // 弹层入场
        fadeUp: {
          "0%": { opacity: "0", transform: "translateY(12px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
      },
      animation: {
        breathe: "breathe 2.4s ease-in-out infinite",
        "pulse-dot": "pulseDot 1.2s ease-in-out infinite",
        wave: "wave 1.2s ease-in-out infinite",
        sweep: "sweep 1.7s cubic-bezier(.5,0,.5,1) infinite",
        "fade-up": "fadeUp .42s cubic-bezier(.16,1,.3,1) both",
      },
    },
  },
  plugins: [],
}