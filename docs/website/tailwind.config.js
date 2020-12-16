module.exports = {
  corePlugins: {
    preflight: false,
  },
  theme: {
    extend: {
      screens: {
        dark: { raw: "(prefers-color-scheme: dark)" },
        // => @media (prefers-color-scheme: dark) { ... }
      },
    },
  },
  variants: {},
  plugins: [require("@tailwindcss/typography")],
};
