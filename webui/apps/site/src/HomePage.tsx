import { useEffect, useState } from "react";
import { api, coverURL, createPlayback, type TrackResult } from "./api";

interface ChartTrack {
	track_id: string;
	platform: string;
	title: string;
	artists?: string[];
	album?: string;
	cover_url?: string;
	url?: string;
}

interface Chart {
	platform: string;
	display_name: string;
	playlist_id: string;
	title: string;
	cover_url?: string;
	tracks: ChartTrack[];
}

interface ChartsResponse {
	charts: Chart[];
	link_mode: "player" | "search" | "platform";
	updated_at: string;
}

/**
 * Landing page: per-platform hot charts as a cover + title grid. Each item is a
 * real anchor so it is keyboard reachable and middle-click/open-in-new-tab
 * works; the player mode intercepts the click to create a session first.
 */
export function HomePage() {
	const [data, setData] = useState<ChartsResponse>();
	const [error, setError] = useState("");
	const [busy, setBusy] = useState("");

	useEffect(() => {
		api<ChartsResponse>("/api/v1/charts")
			.then(setData)
			.catch((e) => setError((e as Error).message));
	}, []);

	function hrefFor(track: ChartTrack, mode: string): string {
		if (mode === "platform") return track.url || "#";
		if (mode === "search")
			return `/search?q=${encodeURIComponent(`${track.title} ${track.artists?.[0] ?? ""}`.trim())}&platform=${encodeURIComponent(track.platform)}`;
		// player mode resolves to a session on click
		return `/search?q=${encodeURIComponent(track.title)}&platform=${encodeURIComponent(track.platform)}`;
	}

	async function openPlayer(track: ChartTrack, event: React.MouseEvent) {
		// Let modified clicks (new tab, etc.) use the plain href.
		if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
			return;
		event.preventDefault();
		const key = `${track.platform}:${track.track_id}`;
		setBusy(key);
		try {
			const seed: TrackResult = {
				track_id: track.track_id,
				platform: track.platform,
				title: track.title,
				artists: track.artists ?? [],
				album: track.album,
				duration_ms: 0,
				cover_url: track.cover_url,
				qualities: [],
			};
			const session = await createPlayback(seed, "high");
			location.href = `/player/${encodeURIComponent(session.session_id)}`;
		} catch (e) {
			setError(`创建播放会话失败：${(e as Error).message}`);
			setBusy("");
		}
	}

	const mode = data?.link_mode ?? "player";

	return (
		<main className="shell">
			<div className="ambient" aria-hidden="true" />
			<header className="hero glass homeHero">
				<span className="eyebrow">MUSICWEB · AMLL</span>
				<h1>今日热榜</h1>
				<p className="lede">
					汇聚各平台实时热门榜单，点击封面即可收听逐字歌词。
				</p>
				<a className="heroSearch" href="/search">
					🔍 搜索歌曲 / 粘贴链接
				</a>
			</header>

			{error && (
				<section className="card">
					<p className="empty">{error}</p>
				</section>
			)}

			{!data && !error && (
				<section className="card">
					<div className="empty">
						<div className="emptyMark">♪</div>
						<p>正在加载榜单…</p>
					</div>
				</section>
			)}

			{data?.charts.length === 0 && (
				<section className="card">
					<div className="empty">
						<div className="emptyMark">♪</div>
						<p>还没有可展示的榜单，请在管理后台配置。</p>
					</div>
				</section>
			)}

			{data?.charts.map((chart) => (
				<section
					className="card chartCard"
					key={`${chart.platform}:${chart.playlist_id}`}
				>
					<div className="sectionHead">
						<h2>
							{chart.title}
							<span className="chartPlatform">{chart.display_name}</span>
						</h2>
						<span>{chart.tracks.length} 首</span>
					</div>
					<div className="chartGrid">
						{chart.tracks.map((track, index) => {
							const key = `${track.platform}:${track.track_id}`;
							return (
								<a
									className="chartItem"
									key={key}
									href={hrefFor(track, mode)}
									target={mode === "platform" ? "_blank" : undefined}
									rel={mode === "platform" ? "noreferrer" : undefined}
									onClick={
										mode === "player"
											? (e) => void openPlayer(track, e)
											: undefined
									}
									title={`${track.title} — ${track.artists?.join(" / ") ?? ""}`}
								>
									<div className="chartCover">
										{track.cover_url ? (
											<img
												src={coverURL(track.cover_url)}
												alt=""
												loading="lazy"
												referrerPolicy="no-referrer"
												onError={(e) => {
													e.currentTarget.style.display = "none";
												}}
											/>
										) : (
											<span className="coverBlank">♪</span>
										)}
										<span className="chartRank">{index + 1}</span>
										{busy === key && <span className="chartBusy">载入中…</span>}
									</div>
									<strong className="chartTitle">{track.title}</strong>
									<span className="chartArtist">
										{track.artists?.join(" / ") || "未知艺人"}
									</span>
								</a>
							);
						})}
					</div>
				</section>
			))}
		</main>
	);
}
