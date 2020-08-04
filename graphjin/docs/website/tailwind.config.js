module.exports = {
  purge: {
    content: ["./pages/index.js"],
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
