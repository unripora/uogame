/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,jsx}'],
  theme: {
    extend: {
      colors: {
        // 主题色通过 CSS 变量驱动（见 src/styles/index.css），此处仅做语义映射
        theme: {
          bg: 'var(--lg-bg)',
          panel: 'var(--lg-panel)',
          border: 'var(--lg-border)',
          text: 'var(--lg-text)',
          sub: 'var(--lg-sub)',
          accent: 'var(--lg-accent)',
          accent2: 'var(--lg-accent-2)',
        },
      },
    },
  },
  plugins: [],
}
