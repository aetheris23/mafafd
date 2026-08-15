import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        border: "hsl(155 12% 15%)",
        input: "hsl(160 12% 12%)",
        ring: "hsl(150 78% 46%)",
        background: "hsl(160 22% 5%)",
        foreground: "hsl(150 20% 90%)",
        primary: {
          DEFAULT: "hsl(150 78% 46%)",
          foreground: "hsl(160 40% 5%)",
        },
        secondary: {
          DEFAULT: "hsl(160 12% 12%)",
          foreground: "hsl(150 15% 88%)",
        },
        destructive: {
          DEFAULT: "hsl(0 72% 51%)",
          foreground: "hsl(0 0% 100%)",
        },
        muted: {
          DEFAULT: "hsl(160 12% 11%)",
          foreground: "hsl(150 12% 55%)",
        },
        accent: {
          DEFAULT: "hsl(160 14% 16%)",
          foreground: "hsl(150 15% 90%)",
        },
        card: {
          DEFAULT: "hsl(160 15% 8%)",
          foreground: "hsl(150 15% 90%)",
        },
      },
      fontFamily: {
        sans: ['"Space Grotesk"', '"Segoe UI"', "system-ui", "sans-serif"],
        display: ['"Space Grotesk"', '"Segoe UI"', "sans-serif"],
        mono: ['"IBM Plex Mono"', '"JetBrains Mono"', '"SFMono-Regular"', "Menlo", "Consolas", "monospace"],
      },
      borderRadius: {
        lg: "0.375rem",
        md: "0.25rem",
        sm: "0.125rem",
      },
      boxShadow: {
        retro: "3px 3px 0 0 hsl(155 25% 6%)",
        "retro-sm": "2px 2px 0 0 hsl(155 25% 6%)",
      },
      keyframes: {
        "pulse-red": {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.4" },
        },
        blink: {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0" },
        },
      },
      animation: {
        "pulse-red": "pulse-red 1.5s ease-in-out infinite",
        blink: "blink 1.1s steps(2, start) infinite",
      },
    },
  },
  plugins: [],
};
export default config;
