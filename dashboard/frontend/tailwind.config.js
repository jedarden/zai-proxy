/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Custom colors for dashboard
        'success': '#22c55e',
        'warning': '#eab308',
        'error': '#ef4444',
        'info': '#3b82f6',
        'chart-1': '#3b82f6',  // Blue
        'chart-2': '#8b5cf6',  // Purple
        'chart-3': '#06b6d4',  // Cyan
        'chart-4': '#f59e0b',  // Amber
        'chart-5': '#ef4444',  // Red
        'chart-6': '#22c55e',  // Green
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
    },
  },
  plugins: [],
}
