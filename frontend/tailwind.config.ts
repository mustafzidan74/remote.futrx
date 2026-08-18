import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Neutral Codex-style dark palette, named so components read clearly.
        ink: {
          50:  "#f4f4f5",
          100: "#e4e4e7",
          200: "#c7c7cc",
          300: "#9b9ba3",
          400: "#8b8b94",
          500: "#3f4047",
          600: "#303137",
          700: "#24252b",
          800: "#18191e",
          900: "#0f1014",
        },
        accent: {
          blue:   "#8ab4ff",
          green:  "#7bd88f",
          red:    "#ff7b72",
          yellow: "#e2b86d",
          orange: "#f0a45d",
          purple: "#b8a8ff",
        },
      },
      fontFamily: {
        // The Arabic faces are appended (never prepended) so Latin text keeps
        // the original stack and only Arabic runs fall through to a face that
        // actually has the glyphs. All of them ship with the OS - no webfont.
        mono: [
          'ui-monospace',
          '"SF Mono"',
          'Menlo',
          'Consolas',
          '"Noto Sans Mono"',
          '"Noto Sans Arabic"',
          'monospace',
        ],
        sans: [
          '-apple-system',
          'system-ui',
          '"Segoe UI"',
          '"Noto Naskh Arabic UI"',
          '"Noto Sans Arabic"',
          '"Geeza Pro"',
          'Tahoma',
          'sans-serif',
        ],
      },
      animation: {
        "pulse-fast": "pulse 1s cubic-bezier(0.4, 0, 0.6, 1) infinite",
      },
    },
  },
  plugins: [],
} satisfies Config;
