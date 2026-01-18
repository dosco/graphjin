import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: './',
  appType: 'spa',
  server: {
    port: 3001,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'build',
    rollupOptions: {
      output: {
        manualChunks: {
          'graphiql': ['graphiql', '@graphiql/toolkit', '@graphiql/plugin-explorer'],
          'swagger': ['swagger-ui-react'],
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
        },
      },
    },
  },
});
