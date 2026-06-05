import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: './',
  appType: 'spa',
  server: {
    port: 3001,
    proxy: {
      '^/api/': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'build',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return undefined;
          }
          if (id.includes('/graphiql') || id.includes('/@graphiql')) {
            return 'graphiql';
          }
          if (id.includes('/swagger-ui-react') || id.includes('/swagger-client') || id.includes('/swagger-ui')) {
            return 'swagger';
          }
          if (id.includes('/react') || id.includes('/react-dom') || id.includes('/react-router')) {
            return 'react-vendor';
          }
          return undefined;
        },
      },
    },
  },
});
