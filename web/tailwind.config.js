/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: [
          '-apple-system',
          'BlinkMacSystemFont',
          '"SF Pro Text"',
          '"SF Pro Display"',
          '"Segoe UI"',
          'Roboto',
          '"PingFang SC"',
          '"Hiragino Sans GB"',
          '"Microsoft YaHei"',
          '"微软雅黑"',
          'sans-serif'
        ],
        mono: [
          '"SF Mono"',
          'SFMono-Regular',
          'ui-monospace',
          'Menlo',
          'Consolas',
          '"Cascadia Mono"',
          '"Segoe UI Mono"',
          'monospace'
        ]
      },
      colors: {
        ios: {
          blue: '#0071E3',
          'blue-hover': '#0077ED',
          green: '#34C759',
          indigo: '#5856D6',
          orange: '#FF9500',
          pink: '#FF2D55',
          purple: '#AF52DE',
          red: '#FF3B30',
          teal: '#5AC8FA',
          yellow: '#FFCC00',
          bg: '#F5F5F7',
          card: '#FFFFFF',
          sidebar: '#F7F7F9',
          label: '#1D1D1F',
          secondary: '#48484A',
          tertiary: '#6E6E73',
          quaternary: '#86868B',
          border: '#D1D1D6',
          separator: 'rgba(0, 0, 0, 0.08)',
          fill: 'rgba(120, 120, 128, 0.16)'
        },
        brand: {
          50: '#F0F7FF',
          100: '#E0EFFF',
          200: '#B9DEFF',
          300: '#7CC2FF',
          400: '#36A2FF',
          500: '#0071E3',
          600: '#005BB5',
          700: '#004C99',
          800: '#004080',
          900: '#003366',
          950: '#001A33',
        }
      },
      boxShadow: {
        'ios': '0 2px 14px rgba(0, 0, 0, 0.05), 0 1px 2px rgba(0, 0, 0, 0.03)',
        'ios-lg': '0 10px 30px rgba(0, 0, 0, 0.09), 0 1px 3px rgba(0, 0, 0, 0.03)',
        'ios-card': '0 1px 3px rgba(0, 0, 0, 0.05), 0 1px 2px -1px rgba(0, 0, 0, 0.03)',
        'xs': '0 1px 2px 0 rgb(0 0 0 / 0.05)',
        '2xs': '0 1px 1px 0 rgb(0 0 0 / 0.03)'
      },
      backdropBlur: {
        '2xs': '2px',
        'ios': '20px'
      }
    },
  },
  plugins: [],
}
