import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'
import semanticHygiene from './eslint-rules/semantic-hygiene/index.cjs'

export default tseslint.config(
  { ignores: ['dist', 'src/components/ui', 'node_modules'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      'leafwiki-semantic-hygiene': semanticHygiene,
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      'leafwiki-semantic-hygiene/no-raw-semantic-identifiers': 'error',
      'leafwiki-semantic-hygiene/no-unsafe-semantic-cast': 'error',
      'leafwiki-semantic-hygiene/require-semantic-status-metadata': 'error',
      ...reactHooks.configs.recommended.rules,
      'no-useless-assignment': 'off',
      'preserve-caught-error': 'off',
      'react-hooks/set-state-in-effect': 'off',
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],
    },
  },
)
