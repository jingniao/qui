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
          '"SF Pro Display"',
          '"SF Pro Text"',
          '"PingFang SC"',
          '"Hiragino Sans GB"',
          '"Microsoft YaHei"',
          'system-ui',
          'sans-serif'
        ]
      },
      colors: {
        ios: {
          blue: '#007AFF',
          'blue-hover': '#0071E3',
          green: '#34C759',
          indigo: '#5856D6',
          orange: '#FF9500',
          pink: '#FF2D55',
          purple: '#AF52DE',
          red: '#FF3B30',
          teal: '#5AC8FA',
          yellow: '#FFCC00',
          bg: '#F2F2F7',
          card: '#FFFFFF',
          sidebar: '#F7F7F9',
          label: '#1C1C1E',
          secondary: '#636366',
          tertiary: '#8E8E93',
          separator: 'rgba(60, 60, 67, 0.12)',
          fill: 'rgba(120, 120, 128, 0.12)'
        },
        brand: {
          50: '#F0F7FF',
          100: '#E0EFFF',
          200: '#B9DEFF',
          300: '#7CC2FF',
          400: '#36A2FF',
          500: '#007AFF',
          600: '#0071E3',
          700: '#005BB5',
          800: '#004C99',
          900: '#004080',
          950: '#002652',
        }
      },
      boxShadow: {
        'ios': '0 2px 14px rgba(0, 0, 0, 0.04), 0 1px 2px rgba(0, 0, 0, 0.02)',
        'ios-lg': '0 10px 30px rgba(0, 0, 0, 0.08), 0 1px 3px rgba(0, 0, 0, 0.02)',
        'ios-card': '0 4px 20px -2px rgba(0, 0, 0, 0.05)',
        'xs': '0 1px 2px 0 rgb(0 0 0 / 0.04)',
        '2xs': '0 1px 1px 0 rgb(0 0 0 / 0.02)'
      },
      backdropBlur: {
        '2xs': '2px',
        'ios': '20px'
      }
    },
  },
  plugins: [],
}
