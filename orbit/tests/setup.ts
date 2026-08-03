import { config } from '@vue/test-utils'

// Nuxt-global components stubbed for plain-vitest mounts. The Icon stub keeps
// the name inspectable so tests can assert which icon rendered.
config.global.stubs = {
  Icon: {
    props: ['name', 'size'],
    template: '<i data-test="icon" :data-icon="name" :data-size="size" />',
  },
  NuxtLink: {
    props: ['to'],
    template: '<a :href="typeof to === \'string\' ? to : \'#\'"><slot /></a>',
  },
}
