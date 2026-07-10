import { useAtomValue, useSetAtom } from "jotai";
import { useEffect, useRef } from "react";
import { toast } from "react-toastify";
import { useFileOpener } from "$/hooks/useFileOpener";
import { amllToTTML, ttmlLyricToAmllResult } from "$/modules/ttml-processor";
import { lyricLinesAtom } from "$/states/main";
import {
	type MusicWebProjectMetadata,
	mergeMusicWebMetadata,
	metadataResolutionSummary,
	musicWebProjectID,
} from "./metadata";

interface Bootstrap {
	project: {
		project_id: string;
		current_revision: number;
		metadata?: MusicWebProjectMetadata;
	};
	audio_url: string;
	seed_lyric_url: string;
	revision: number;
}

async function checkedFetch(url: string, init?: RequestInit) {
	const response = await fetch(url, init);
	if (response.status === 401) {
		location.href = `/admin/login?next=${encodeURIComponent(location.pathname)}`;
		throw new Error("需要管理员登录");
	}
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error(data.error || `HTTP ${response.status}`);
	}
	return response;
}

async function fetchAudio(url: string) {
	for (let attempt = 0; attempt < 180; attempt += 1) {
		const response = await fetch(url);
		if (response.ok) return response;
		if (response.status !== 409)
			throw new Error(`音频加载失败：HTTP ${response.status}`);
		await new Promise((resolve) => setTimeout(resolve, 1000));
	}
	throw new Error("音频准备超时");
}

/**
 * MusicWeb bootstrap/autosave bridge. Upstream's IndexedDB autosave remains
 * active; this component adds a second optimistic server snapshot channel.
 */
export function MusicWebBridge() {
	const id = musicWebProjectID();
	const { openFile } = useFileOpener();
	const lyrics = useAtomValue(lyricLinesAtom);
	const setLyrics = useSetAtom(lyricLinesAtom);
	const initialized = useRef(false);
	const revision = useRef(0);
	const timer = useRef<number | undefined>(undefined);

	useEffect(() => {
		if (!id || initialized.current) return;
		initialized.current = true;
		void (async () => {
			const bootstrap = await checkedFetch(
				`/api/v1/studio/projects/${encodeURIComponent(id)}/bootstrap`,
			).then((r) => r.json() as Promise<Bootstrap>);
			revision.current = bootstrap.revision;
			const lyricText = await checkedFetch(bootstrap.seed_lyric_url).then((r) =>
				r.text(),
			);
			await openFile(
				new File([lyricText], `${id}.ttml`, { type: "application/ttml+xml" }),
				"ttml",
			);
			setLyrics((current) =>
				mergeMusicWebMetadata(current, bootstrap.project.metadata),
			);
			const audioResponse = await fetchAudio(bootstrap.audio_url);
			const blob = await audioResponse.blob();
			const ext = blob.type.includes("flac")
				? "flac"
				: blob.type.includes("mp4")
					? "m4a"
					: "mp3";
			void openFile(new File([blob], `music.${ext}`, { type: blob.type }), ext);
			const summary = metadataResolutionSummary(bootstrap.project.metadata);
			toast.success(
				`已自动导入音频和逐字歌词；元数据匹配 ${summary.matched}/${summary.total} 个平台，ISRC ${summary.isrcs} 个`,
			);
			if (summary.unresolved.length > 0) {
				toast.warning(
					`以下平台未达到自动确认阈值：${summary.unresolved.join("、")}；请在元数据编辑器确认候选`,
				);
			}
		})().catch((error) => toast.error(`项目导入失败：${error.message}`));
	}, [id, openFile, setLyrics]);

	useEffect(() => {
		if (!id || !initialized.current || lyrics.lyricLines.length === 0) return;
		window.clearTimeout(timer.current);
		timer.current = window.setTimeout(() => {
			const result = amllToTTML(ttmlLyricToAmllResult(lyrics));
			if (!result.success) return;
			void checkedFetch(
				`/api/v1/studio/projects/${encodeURIComponent(id)}/revisions`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({
						expected_revision: revision.current,
						content: result.data,
						metadata: lyrics.metadata,
					}),
				},
			)
				.then((response) => response.json())
				.then((data: { revision: number }) => {
					revision.current = data.revision;
				})
				.catch((error) => {
					if (String(error.message).includes("冲突"))
						toast.warning("服务端发现新版本，请导出当前歌词后刷新页面");
					else toast.error(`服务端自动保存失败：${error.message}`);
				});
		}, 2000);
		return () => window.clearTimeout(timer.current);
	}, [id, lyrics]);

	return null;
}
