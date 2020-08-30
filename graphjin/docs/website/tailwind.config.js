module.exports = {
  corePlugins: {
    preflight: false,
  },
  purge: {
    enabled: true,
    content: ["./src/pages/*.js", "./src/pages/components/*.jsx"],
    options: {
      whitelist: ["dark"],
    },
  },
  theme: {
    typography: {
      default: {
        css: {
          color: "#222",
        },
      },
    },
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
