import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const devServerHost = "localhost";
const backendServerPort = 45678;
const wsgwPort = 45679;

// https://vitejs.dev/config/
export default defineConfig({
	plugins: [react()],
	server: {
		host: "0.0.0.0",
		cors: false,
		open: true,
		proxy: {
			"/config": {
				target: `http://${devServerHost}:${backendServerPort}`,
				changeOrigin: true
			},
			"/user": {
				target: `http://${devServerHost}:${backendServerPort}`,
				changeOrigin: true
			},
			"/ws": {
				target: `http://${devServerHost}:${wsgwPort}`,
				changeOrigin: true
			},
			"/api": {
				target: `http://${devServerHost}:${backendServerPort}`,
				changeOrigin: true
			}
		}
	},
	test: {
		globals: true,
		environment: "jsdom",
		setupFiles: "src/setupTests",
		mockReset: true
	}
});
