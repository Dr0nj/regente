# Regente Lite — MVP

Plataforma serverless de orquestração de jobs com interface visual inspirada em Control-M.

## Stack

- **Frontend**: React + TypeScript + Vite
- **UI**: Tailwind CSS + componentes customizados
- **Canvas**: React Flow (@xyflow/react) para drag-and-drop visual
- **Auth**: AWS Cognito (via Amplify Auth)
- **Data**: DynamoDB (via Amplify Data / AppSync GraphQL)
- **Backend**: AWS Amplify Gen 2

## Como rodar localmente

### Pré-requisitos

1. Node.js 18+
2. Conta AWS configurada (`aws configure`)
3. Amplify CLI: `npm install -g @aws-amplify/backend-cli`

### Instalar dependências

```bash
cd app
npm install
```

### Iniciar backend sandbox (cria recursos AWS reais para dev)

```bash
cd app
npx ampx sandbox
```

Isso vai:
- Criar um Cognito User Pool (auth)
- Criar tabelas DynamoDB (Job, Execution)
- Criar API GraphQL (AppSync)
- Gerar `amplify_outputs.json` com as configs

### Iniciar frontend

Em outro terminal:

```bash
cd app
npm run dev
```

Acesse `http://localhost:5173`

### Deploy para produção

```bash
cd app
npx ampx pipeline-deploy --branch main --app-id SEU_APP_ID
```

Ou use o Amplify Console na AWS para CI/CD automático via GitHub.

## Estrutura do projeto

```
app/
├── amplify/
│   ├── auth/resource.ts          # Config Cognito
│   ├── data/resource.ts          # Modelos DynamoDB (Job, Execution)
│   └── backend.ts                # Backend principal
├── src/
│   ├── main.tsx                  # Entry point + config Amplify
│   ├── App.tsx                   # Layout + Authenticator
│   ├── components/
│   │   ├── Dashboard.tsx         # Sidebar + stats + canvas
│   │   ├── JobCanvas.tsx         # Canvas React Flow
│   │   └── nodes/
│   │       ├── LambdaNode.tsx    # Node visual Lambda
│   │       ├── BatchNode.tsx     # Node visual Batch
│   │       └── ChoiceNode.tsx    # Node visual Choice
│   └── index.css                 # Tailwind + React Flow styles
└── amplify_outputs.json          # Gerado pelo sandbox (não comitar)
```

## Roadmap

- [x] Phase 1: Projeto base (auth, modelos, canvas, dashboard)
- [ ] Phase 2: Formulário de criação de job
- [ ] Phase 3: Execução de jobs via Step Functions
- [ ] Phase 4: Status em tempo real e alertas
- [ ] Phase 5: Calendários e dependências entre jobs
# React + TypeScript + Vite

This template provides a minimal setup to get React working in Vite with HMR and some ESLint rules.

Currently, two official plugins are available:

- [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react) uses [Oxc](https://oxc.rs)
- [@vitejs/plugin-react-swc](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react-swc) uses [SWC](https://swc.rs/)

## React Compiler

The React Compiler is not enabled on this template because of its impact on dev & build performances. To add it, see [this documentation](https://react.dev/learn/react-compiler/installation).

## Expanding the ESLint configuration

If you are developing a production application, we recommend updating the configuration to enable type-aware lint rules:

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...

      // Remove tseslint.configs.recommended and replace with this
      tseslint.configs.recommendedTypeChecked,
      // Alternatively, use this for stricter rules
      tseslint.configs.strictTypeChecked,
      // Optionally, add this for stylistic rules
      tseslint.configs.stylisticTypeChecked,

      // Other configs...
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

You can also install [eslint-plugin-react-x](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-x) and [eslint-plugin-react-dom](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-dom) for React-specific lint rules:

```js
// eslint.config.js
import reactX from 'eslint-plugin-react-x'
import reactDom from 'eslint-plugin-react-dom'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...
      // Enable lint rules for React
      reactX.configs['recommended-typescript'],
      // Enable lint rules for React DOM
      reactDom.configs.recommended,
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```
