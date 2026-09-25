/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './components/**/*.{vue,js,ts}',
    './layouts/**/*.vue',
    './pages/**/*.vue',
    './app.vue',
    './data/**/*.ts'
  ],
  theme: {
    extend: {
      colors: {
        suno: {
          yellow: '#ffed29',
          bg: '#121212',
          gray: {
            600: '#404040',
            700: '#2a2a2a',
            800: '#1f1f1f',
            900: '#171717'
          },
          text: {
            light: '#f0f0f0',
            white: '#ffffff'
          }
        }
      },
      fontFamily: {
        sans: ['Inter', 'PingFang SC', 'Microsoft YaHei', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Consolas', 'Monaco', 'monospace']
      }
    }
  },
  plugins: []
}
