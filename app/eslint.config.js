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

      // ── react-hooks/react-refresh: WARN, não ERROR (decisão da auditoria 2026-07-21) ──
      // Estas regras vieram no set "recommended" novo do plugin (era React
      // Compiler) e sinalizam PADRÕES, não bugs. O app tem código LOAD-BEARING
      // que elas acusam e que já mordeu regressão quando mexido:
      //   - set-state-in-effect / refs → a sincronização de dados e a CÂMERA do
      //     canvas (useCanvasCamera.apply() é o único caminho de escrita; mexer
      //     ali divergia a trava — ver mente regente-canvas-camera-model).
      //   - immutability → acumulador em map() no cálculo de offsets da sidebar
      //     windowed (funciona; reescrever não paga).
      //   - only-export-components → só afeta Fast Refresh (DX de dev), nunca prod.
      // Ficam como WARN: visíveis pra quem for refatorar, mas NÃO bloqueiam o CI
      // (o gate é erro-only). Correção real dessas = projeto próprio, não auditoria.
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/refs': 'warn',
      'react-hooks/immutability': 'warn',
      'react-refresh/only-export-components': 'warn',
    },
  },
])
