
const js = require('@eslint/js')
const tseslint = require('typescript-eslint')
const prettier = require('eslint-config-prettier')
const pluginPrettier = require('eslint-plugin-prettier')
const semanticHygiene = require('../ui/leafwiki-ui/eslint-rules/semantic-hygiene/index.cjs')

module.exports = tseslint.config(
  js.configs.recommended,
  ...tseslint.configs.recommended,
  prettier,
  {
    files: ['tests/**/*.ts', 'playwright.config.ts'],
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: {
        project: './tsconfig.json',
        sourceType: 'module'
      }
    },
    plugins: {
      'leafwiki-semantic-hygiene': semanticHygiene,
      prettier: pluginPrettier
    },
    rules: {
      'leafwiki-semantic-hygiene/no-localized-prose-assertions': 'error',
      'prettier/prettier': ['error'],

      'quotes': ['error', 'single'],
      'semi': ['error', 'always'],
      'no-console': 'off',
      'no-var': 'off',

      'sort-imports': [
        'warn',
        {
          ignoreCase: false,
          ignoreDeclarationSort: true,
          ignoreMemberSort: false,
          memberSyntaxSortOrder: ['none', 'all', 'multiple', 'single'],
          allowSeparatedGroups: true
        }
      ]
    },
    ignores: ['node_modules', 'dist']
  },
  {
    files: ['pages/**/*.ts'],
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: {
        sourceType: 'module'
      }
    },
    plugins: {
      'leafwiki-semantic-hygiene': semanticHygiene
    },
    rules: {
      'leafwiki-semantic-hygiene/no-localized-prose-assertions': 'error'
    }
  }
)
