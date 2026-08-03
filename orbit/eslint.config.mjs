import withNuxt from './.nuxt/eslint.config.mjs'

export default withNuxt({
  rules: {
    'vue/multi-word-component-names': 'off',
    // readability: a blank line after every if/for/while/do/try block (an
    // if…else chain is one statement, so else/else-if stay attached)
    '@stylistic/padding-line-between-statements': ['error',
      { blankLine: 'always', prev: ['if', 'for', 'while', 'do', 'try'], next: '*' },
    ],
  },
})
