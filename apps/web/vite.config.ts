import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
const target = "http://127.0.0.1:7331";
export default defineConfig({
  plugins: [
    vue(),
    {
      name: "local-api-origin",
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          if (
            req.url?.startsWith("/api/") &&
            req.headers.origin &&
            req.headers.origin !== "http://" + req.headers.host
          ) {
            res.statusCode = 403;
            res.end("Forbidden origin");
            return;
          }
          next();
        });
      },
    },
  ],
  server: {
    host: "127.0.0.1",
    proxy: {
      "/api": {
        target,
        changeOrigin: true,
        ws: true,
        configure(proxy) {
          proxy.on("proxyReq", (proxyReq, req) => {
            if (req.headers.origin) proxyReq.setHeader("Origin", target);
          });
          proxy.on("proxyReqWs", (proxyReq, req) => {
            if (req.headers.origin !== "http://" + req.headers.host) {
              proxyReq.destroy();
              return;
            }
            proxyReq.setHeader("Origin", target);
          });
        },
      },
    },
  },
});
