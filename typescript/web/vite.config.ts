import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // Данные живут у Go, страница у Vite. Без этого запрос `api/money` уходит на
    // 5173, Vite отвечает своей index.html - потому что маршруты страницы живут в
    // браузере и всё неизвестное получает страницу, - а разбор JSON спотыкается о
    // `<!doctype` и раздел показывает отказ.
    //
    // Проксирование делает разработку такой же, как бой: один источник, тот же
    // путь, никаких заголовков для чужого домена.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/healthz': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
