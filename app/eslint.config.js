import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Prefixo `_` = intencionalmente não usado (stubs de interface, params
      // ignorados) — convenção do projeto; sem isto o lint enterra os erros
      // reais em dezenas de falsos positivos.
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],

      // ── react-hooks/react-refresh: ERROR (catraca RH-4, 2026-07-21) ──
      // A trilha RH (roadmap §RH) passou o app por estas 4 regras site-a-site:
      // consertou o mecânico (immutability da sidebar, refs de resizable/camera-mirror,
      // A1/A2/A3 das cargas iniciais) e ANOTOU cada caso load-bearing deliberado com
      // `eslint-disable-next-line <regra> -- <motivo>; ver roadmap §RH` (câmera do
      // canvas, snapshot do autosave/invariante 4, resets on-dep-change do drawer M1,
      // API pública que convive com componente no módulo). Com 0 violação restante,
      // sobem pra ERROR: agora o gate do CI (`npm run lint`) BLOQUEIA qualquer
      // violação nova — código novo não regride por copy-paste. Toda exceção futura
      // tem que ser um disable ANOTADO com motivo, nunca um warn silencioso.
      'react-hooks/set-state-in-effect': 'error',
      'react-hooks/refs': 'error',
      'react-hooks/immutability': 'error',
      'react-refresh/only-export-components': 'error',
    },
  },
])
