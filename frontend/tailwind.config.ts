import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        border: "hsl(220 13% 12%)",
        input: "hsl(220 13% 16%)",
        ring: "hsl(152 60% 45%)",
        background: "hsl(220 30% 7%)",
        foreground: "hsl(220 15% 92%)",
        primary: {
          DEFAULT: "hsl(152 60% 45%)",
          foreground: "hsl(220 30% 7%)",
        },
        secondary: {
          DEFAULT: "hsl(220 13% 16%)",
          foreground: "hsl(220 15% 92%)",
        },
        destructive: {
          DEFAULT: "hsl(0 72% 51%)",
          foreground: "hsl(0 0% 100%)",
        },
        muted: {
          DEFAULT: "hsl(220 13% 14%)",
          foreground: "hsl(220 10% 60%)",
        },
        accent: {
          DEFAULT: "hsl(220 13% 20%)",
          foreground: "hsl(220 15% 92%)",
        },
        card: {
          DEFAULT: "hsl(220 25% 9%)",
          foreground: "hsl(220 15% 92%)",
        },
      },
      borderRadius: {
        lg: "0.5rem",
        md: "0.375rem",
        sm: "0.25rem",
      },
      keyframes: {
        "pulse-red": {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.4" },
        },
      },
      animation: {
        "pulse-red": "pulse-red 1.5s ease-in-out infinite",
      },
    },
  },
  plugins: [],
};
export default config;
