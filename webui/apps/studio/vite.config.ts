import { exec } from "node:child_process";
import { resolve } from "node:path";
import babel from "@rolldown/plugin-babel";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import jotaiDebugLabel from "jotai-babel/plugin-debug-label";
import jotaiReactRefresh from "jotai-babel/plugin-react-refresh";
import { defineConfig, type PluginOption } from "vite";
import i18nextLoader from "vite-plugin-i18next-loader";

const isProduction = process.env.NODE_ENV === "production";

const plugins: PluginOption = [
	react(),
	babel({
		presets: [reactCompilerPreset()],
		plugins: isProduction ? [] : [jotaiDebugLabel, jotaiReactRefresh],
	}),
	i18nextLoader({
		paths: ["./locales"],
		namespaceResolution: "basename",
	}),
	{
		name: "buildmeta",
		async resolveId(id) {
			if (id === "virtual:buildmeta") {
				return id;
			}
		},
		async load(id) {
			if (id === "virtual:buildmeta") {
				let gitCommit = "unknown";

				try {
					gitCommit = await new Promise<string>((resolve, reject) =>
						exec("git rev-parse HEAD", (err, stdout) => {
							if (err) {
								reject(err);
							} else {
								resolve(stdout.trim());
							}
						}),
					);
				} catch {}

				return `
					export const BUILD_TIME = "${new Date().toISOString()}";
					export const GIT_COMMIT = "${gitCommit}";
				`;
			}
		},
	},
];

// https://vitejs.dev/config/
export default defineConfig({
	plugins,
	base: process.env.TAURI_ENV_PLATFORM ? "/" : "/studio/",
	clearScreen: false,
	server: {
		headers: {
			"Cross-Origin-Embedder-Policy": "require-corp",
			"Cross-Origin-Opener-Policy": "same-origin",
		},
		strictPort: true,
	},
	envPrefix: ["VITE_", "TAURI_", "AMLL_", "SENTRY_"],
	build: {
		outDir: "../../dist/studio",
		emptyOutDir: true,
		// produce sourcemaps
		sourcemap: true,

		rolldownOptions: {
			// Suppress `Module "node:module" has been externalized for browser compatibility`
			onLog(level, log, defaultHandler) {
				if (log.message?.includes("node:module")) return;
				defaultHandler(level, log);
			},

			output: {
				codeSplitting: {
					groups: [
						{
							name: "react-vendor",
							test: /node_modules[\\/](react|react-dom)[\\/]/,
							priority: 20,
						},
						{
							name: "radix-vendor",
							test: /node_modules[\\/]@radix/,
							priority: 15,
						},
						{
							name: "amll-vendor",
							test: /node_modules[\\/]@applemusic-like-lyrics/,
							priority: 10,
						},
						{
							name: "vendor",
							test(id) {
								// 排除掉 hyphen 以便动态导入能够工作
								return id.includes("node_modules") && !id.includes("hyphen");
							},
							priority: 10,
						},
					],
				},
			},
		},
	},
	resolve: {
		alias: {
			$: resolve(__dirname, "src"),
		},
		// pnpm can otherwise resolve hooks used by jotai-immer through a second
		// physical React module. React then sees a null hook dispatcher at runtime.
		dedupe: ["react", "react-dom", "jotai", "jotai-immer"],
	},
	worker: {
		format: "es",
	},
});
