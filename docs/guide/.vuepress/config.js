let ogprefix = 'og: http://ogp.me/ns#'
let title = 'Super Graph'
let description = 'Fetch data without code'
let color = '#f42525'

module.exports = {
  title: title,
  description: description,

  themeConfig: {
    logo: '/hologram.svg',
    nav: [
      { text: 'Docs', link: '/guide' },
      { text: 'Deploy', link: '/deploy' },
      { text: 'Internals', link: '/internals' },
      { text: 'Github', link: 'https://github.com/dosco/super-graph' },
      { text: 'Docker', link: 'https://hub.docker.com/r/dosco/super-graph/builds' },
      { text: 'Join Chat', link: 'https://discord.com/invite/23Wh7c' },
    ],
    serviceWorker: {
      updatePopup: true
    },

    head: [
      //['link', { rel: 'icon', href: `/assets/favicon.ico` }],
      ['meta', { prefix: ogprefix, property: 'og:title', content: title }],
      ['meta', { prefix: ogprefix, property: 'twitter:title', content: title }],
      ['meta', { prefix: ogprefix, property: 'og:type', content: 'website' }],
      ['meta', { prefix: ogprefix, property: 'og:url', content: 'https://supergraph.dev' }],
      ['meta', { prefix: ogprefix, property: 'og:description', content: description }],
      //['meta', { prefix: ogprefix, property: 'og:image', content: 'https://wireupyourfrontend.com/assets/logo.png' }],
      // ['meta', { name: 'apple-mobile-web-app-capable', content: 'yes' }],
      // ['meta', { name: 'apple-mobile-web-app-status-bar-style', content: 'black' }],
      // ['link', { rel: 'apple-touch-icon', href: `/assets/apple-touch-icon.png` }],
      // ['link', { rel: 'mask-icon', href: '/assets/safari-pinned-tab.svg', color: color }],
      // ['meta', { name: 'msapplication-TileImage', content: '/assets/mstile-150x150.png' }],
      // ['meta', { name: 'msapplication-TileColor', content: color }],
  ],
  },

  postcss: {
    plugins: [
      require('postcss-import'),
      require('tailwindcss'),
      require('postcss-nested'),
      require('autoprefixer')
    ]
  },

  plugins: [
    '@vuepress/plugin-nprogress',
  ]
}
