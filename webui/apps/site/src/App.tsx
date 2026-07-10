import { useEffect, useMemo, useState } from "react";
import { api, coverURL, createPlayback, type DownloadJob, type PlatformInfo, type TrackResult } from "./api";
import { PlayerPage } from "./PlayerPage";

type JobView = DownloadJob & { key: string };

function routeSession(): string {
  const match = location.pathname.match(/^\/player\/([^/]+)/);
  return match ? decodeURIComponent(match[1]) : "";
}

export function App() {
  const session = routeSession();
  if (session) return <PlayerPage sessionId={session} />;
  return <SearchPage />;
}

function SearchPage() {
  const [platforms, setPlatforms] = useState<PlatformInfo[]>([]);
  const [platform, setPlatform] = useState("netease");
  const [query, setQuery] = useState("");
  const [link, setLink] = useState("");
  const [results, setResults] = useState<TrackResult[]>([]);
  const [jobs, setJobs] = useState<JobView[]>([]);
  const [message, setMessage] = useState("正在加载平台列表…");

  useEffect(() => {
    api<{ platforms: PlatformInfo[] }>("/api/v1/platforms")
      .then(({ platforms }) => {
        const available = platforms.filter((item) => item.capabilities?.search);
        setPlatforms(available);
        if (available.length) setPlatform(available.find((item) => item.name === "netease")?.name || available[0].name);
        setMessage("输入关键词或粘贴歌曲链接开始。");
      })
      .catch((error) => setMessage(`平台加载失败：${error.message}`));
  }, []);

  async function search() {
    if (!query.trim()) return;
    setMessage("搜索中…");
    try {
      const data = await api<{ results: TrackResult[] }>(`/api/v1/search?platform=${encodeURIComponent(platform)}&q=${encodeURIComponent(query)}&limit=20`);
      setResults(data.results || []);
      setMessage(`共 ${data.results?.length || 0} 条结果`);
    } catch (error) { setMessage(`搜索失败：${(error as Error).message}`); }
  }

  async function parseLink() {
    if (!link.trim()) return;
    setMessage("正在解析链接…");
    try {
      const data = await api<{ result: TrackResult }>(`/api/v1/parse?url=${encodeURIComponent(link)}`);
      setResults(data.result ? [data.result] : []);
      setMessage(data.result ? "链接解析成功" : "未解析到歌曲");
    } catch (error) { setMessage(`解析失败：${(error as Error).message}`); }
  }

  async function download(track: TrackResult, quality: string) {
    const pending: JobView = { key: crypto.randomUUID(), job_id: "", status: "creating", progress: 0, quality };
    setJobs((old) => [pending, ...old]);
    try {
      const job = await api<DownloadJob>("/api/v1/downloads", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ platform: track.platform, track_id: track.track_id, quality })
      });
      setJobs((old) => old.map((item) => item.key === pending.key ? { ...job, key: pending.key } : item));
      pollJob(job.job_id, pending.key);
    } catch (error) {
      setJobs((old) => old.map((item) => item.key === pending.key ? { ...item, status: "failed", error: (error as Error).message } : item));
    }
  }

  async function pollJob(id: string, key: string) {
    try {
      const job = await api<DownloadJob>(`/api/v1/downloads/${encodeURIComponent(id)}`);
      setJobs((old) => old.map((item) => item.key === key ? { ...job, key } : item));
      if (!["ready", "failed", "expired"].includes(job.status)) setTimeout(() => pollJob(id, key), 1000);
    } catch (error) {
      setJobs((old) => old.map((item) => item.key === key ? { ...item, status: "failed", error: (error as Error).message } : item));
    }
  }

  async function listen(track: TrackResult, quality: string) {
    setMessage(`正在准备在线播放：${track.title}`);
    try {
      const session = await createPlayback(track, quality);
      location.href = `/player/${encodeURIComponent(session.session_id)}`;
    } catch (error) { setMessage(`创建播放会话失败：${(error as Error).message}`); }
  }

  async function studio(track: TrackResult, quality: string) {
    try {
      const project = await api<{ project_id: string }>("/api/v1/studio/projects", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ platform: track.platform, track_id: track.track_id, quality })
      });
      location.href = `/studio/${encodeURIComponent(project.project_id)}`;
    } catch (error) {
      if ((error as Error).message.includes("管理员")) location.href = "/admin/login";
      else setMessage(`创建歌词项目失败：${(error as Error).message}`);
    }
  }

  return <main className="shell">
    <header className="hero">
      <div><span className="eyebrow">MUSICWEB · AMLL</span><h1>找到音乐，也看见每一个字。</h1><p>跨平台搜索、在线播放、逐字歌词和歌词制作。</p></div>
      <div className="searchGrid">
        <select value={platform} onChange={(event) => setPlatform(event.target.value)}>{platforms.map((item) => <option key={item.name} value={item.name}>{item.emoji || "🎵"} {item.display_name || item.name}</option>)}</select>
        <input value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => event.key === "Enter" && search()} placeholder="歌曲 / 歌手 / 专辑" />
        <button onClick={search}>搜索</button>
      </div>
      <div className="linkGrid"><input value={link} onChange={(event) => setLink(event.target.value)} onKeyDown={(event) => event.key === "Enter" && parseLink()} placeholder="粘贴歌曲链接，自动识别平台"/><button className="ghost" onClick={parseLink}>解析链接</button></div>
    </header>

    {jobs.length > 0 && <section className="card"><h2>下载任务</h2>{jobs.map((job) => <div className="job" key={job.key}><div><strong>{job.title || "正在创建任务"}</strong><small>{job.quality} · {job.status}{job.error ? ` · ${job.error}` : ""}</small><div className="bar"><i style={{ width: `${job.progress || 0}%` }}/></div></div>{job.status === "ready" && <a href={`/api/v1/downloads/${job.job_id}/file`}>下载文件</a>}</div>)}</section>}

    <section className="card"><div className="sectionHead"><h2>歌曲</h2><span>{message}</span></div><div>{results.map((track) => <TrackRow key={`${track.platform}:${track.track_id}`} track={track} onDownload={download} onListen={listen} onStudio={studio}/>)}</div></section>
  </main>;
}

function TrackRow({ track, onDownload, onListen, onStudio }: { track: TrackResult; onDownload: (track: TrackResult, quality: string) => void; onListen: (track: TrackResult, quality: string) => void; onStudio: (track: TrackResult, quality: string) => void }) {
  const [quality, setQuality] = useState(track.qualities?.[0]?.value || "high");
  const [lyricFormat, setLyricFormat] = useState("ttml");
  const [translation, setTranslation] = useState(true);
  const [roma, setRoma] = useState(true);
  const [lyricSource, setLyricSource] = useState("");
  const artists = useMemo(() => track.artists?.join(" / ") || "未知艺人", [track.artists]);
  async function downloadLyric() {
    setLyricSource("正在解析歌词来源…");
    try {
      const result = await api<{ asset: { source: string; author?: string; word_synced: boolean; confidence: number } }>(`/api/v1/lyrics/${encodeURIComponent(track.platform)}/${encodeURIComponent(track.track_id)}?format=${encodeURIComponent(lyricFormat)}&translation=${translation ? 1 : 0}&roma=${roma ? 1 : 0}`);
      const asset = result.asset;
      setLyricSource(`${asset.source}${asset.author ? ` · ${asset.author}` : ""} · ${asset.word_synced ? "逐字" : "逐行"} · ${asset.confidence}%`);
      const href = `/api/v1/lyrics/file?platform=${encodeURIComponent(track.platform)}&track_id=${encodeURIComponent(track.track_id)}&format=${encodeURIComponent(lyricFormat)}&translation=${translation ? 1 : 0}&roma=${roma ? 1 : 0}`;
      const anchor = document.createElement("a"); anchor.href = href; anchor.click();
    } catch (error) { setLyricSource(`歌词失败：${(error as Error).message}`); }
  }
  return <article className="track"><div className="cover">{track.cover_url ? <img src={coverURL(track.cover_url)} referrerPolicy="no-referrer" onError={(event) => event.currentTarget.style.display = "none"}/> : null}</div><div className="trackInfo"><strong>{track.title}</strong><span>{artists}</span><small>{track.album || "未知专辑"} · {track.platform}</small>{lyricSource && <small>{lyricSource}</small>}</div><div className="trackActions"><select value={quality} onChange={(event) => setQuality(event.target.value)}>{(track.qualities || []).map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select><button onClick={() => onListen(track, quality)}>在线播放</button><button className="ghost" onClick={() => onDownload(track, quality)}>下载</button><button className="ghost" onClick={() => onStudio(track, quality)}>制作歌词</button><select value={lyricFormat} onChange={(event) => setLyricFormat(event.target.value)}>{["ttml","lrc","yrc","qrc","lys","elrc","ass","srt","amjson","txt"].map((format) => <option key={format}>{format.toUpperCase()}</option>)}</select><label><input type="checkbox" checked={translation} onChange={(event) => setTranslation(event.target.checked)}/>翻译</label><label><input type="checkbox" checked={roma} onChange={(event) => setRoma(event.target.checked)}/>罗马音</label><button className="ghost" onClick={downloadLyric}>下载歌词</button></div></article>;
}
