import path from "path"
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    // As views/diálogos pesados já saem em chunks sob demanda (React.lazy em
    // V2Preview). O chunk principal restante (~600 kB) é dominado pelo React
    // Flow: é o canvas do Design E do Monitoring — carrega no primeiro paint das
    // duas telas, então lazy-load dele não adianta (ele É o conteúdo inicial).
    // Limite ajustado pra refletir esse piso real e manter o build sem ruído.
    chunkSizeWarningLimit: 650,
  },
})
