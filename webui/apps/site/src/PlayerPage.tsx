import { useEffect, useMemo, useRef, useState } from "react";
import { LyricPlayer } from "@applemusic-like-lyrics/react";
import { parseTTML } from "@applemusic-like-lyrics/lyric";
import type { LyricLine } from "@applemusic-like-lyrics/core";
import { api, coverURL, type LyricInfo, type PlaybackSession } from "./api";

const finalStatuses = new Set(["ready", "failed", "expired"]);

function formatTime(milliseconds: number) {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return "0:00";
  const seconds = Math.floor(milliseconds / 1000);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

export function PlayerPage({ sessionId }: { sessionId: string }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [session, setSession] = useState<PlaybackSession>();
  const [lines, setLines] = useState<LyricLine[]>([]);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [volume, setVolume] = useState(0.85);
  const [muted, setMuted] = useState(false);
  const [showTranslation, setShowTranslation] = useState(true);
  const [showRoma, setShowRoma] = useState(true);
  const [lowPower, setLowPower] = useState(() => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false);
  const [source, setSource] = useState("正在准备歌词…");

  useEffect(() => {
    let cancelled = false;
    let timer = 0;
    async function load() {
      try {
        const current = await api<PlaybackSession>(`/api/v1/playback/sessions/${encodeURIComponent(sessionId)}`);
        if (cancelled) return;
        setSession(current);
        if (!finalStatuses.has(current.status)) {
          timer = window.setTimeout(load, 1000);
          return;
        }
        if (current.status !== "ready") {
          setSource(current.error || "音频准备失败");
          return;
        }
        const lyric = await api<LyricInfo>(current.lyric_url);
        if (cancelled) return;
        setSource(`${lyric.source} · ${lyric.match_type} · ${lyric.confidence}%${lyric.author ? ` · ${lyric.author}` : ""}`);
        setLines(parseTTML(lyric.content).lines);
      } catch (error) {
        if (!cancelled) setSource(`加载失败：${(error as Error).message}`);
      }
    }
    void load();
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [sessionId]);

  // Update AMLL only while audio is moving. The old implementation kept a
  // permanent 60 FPS loop even while paused; 30/15 FPS is visually sufficient
  // and much friendlier to Safari and mobile devices.
  useEffect(() => {
    if (!playing) return;
    let frame = 0;
    let lastPaint = 0;
    const interval = lowPower ? 66 : 33;
    const update = (now: number) => {
      if (audio.current && now - lastPaint >= interval) {
        setTime(Math.round(audio.current.currentTime * 1000));
        lastPaint = now;
      }
      frame = requestAnimationFrame(update);
    };
    frame = requestAnimationFrame(update);
    return () => cancelAnimationFrame(frame);
  }, [playing, lowPower]);

  const visibleLines = useMemo(() => lines.map((line) => ({
    ...line,
    translatedLyric: showTranslation ? line.translatedLyric : "",
    romanLyric: showRoma ? line.romanLyric : "",
    words: line.words.map((word) => ({ ...word })),
  })), [lines, showTranslation, showRoma]);

  const cover = coverURL(session?.cover_url);
  const ready = session?.status === "ready";
  const measuredDuration = Number.isFinite(duration) && duration > 0 ? duration : 0;
  const effectiveDuration = measuredDuration || session?.duration_ms || 0;

  function seek(nextMS: number) {
    const next = Math.max(0, Math.min(nextMS, effectiveDuration || nextMS));
    if (audio.current) audio.current.currentTime = next / 1000;
    setTime(next);
  }

  async function togglePlay() {
    if (!audio.current || !ready) return;
    try {
      if (audio.current.paused) await audio.current.play();
      else audio.current.pause();
    } catch (error) {
      setSource(`播放失败：${(error as Error).message}`);
    }
  }

  function updateVolume(next: number) {
    setVolume(next);
    setMuted(next === 0);
    if (audio.current) {
      audio.current.volume = next;
      audio.current.muted = next === 0;
    }
  }

  return <main className={`amllPage ${lowPower ? "lowPower" : ""}`}>
    <div className="amllBackdrop" style={{ backgroundImage: cover ? `url(${cover})` : undefined }}/>
    <div className="amllColorWash"/>
    <header className="playerTopbar">
      <a className="playerBack" href="/" aria-label="返回搜索">‹ <span>返回音乐库</span></a>
      <div className="playerBrand">MUSICWEB <b>AMLL</b></div>
      <button className="iconPill" onClick={() => setLowPower((value) => !value)} title="切换视觉效果">{lowPower ? "标准效果" : "节能模式"}</button>
    </header>

    <section className="playerLeft">
      <div className="playerCoverShell">
        {cover ? <img className="playerCover" src={cover} alt={session?.album || session?.title || "专辑封面"} referrerPolicy="no-referrer"/> : <div className="playerCover placeholder">♪</div>}
      </div>
      <div className="playerMeta">
        <div><h1>{session?.title || "正在准备音乐…"}</h1><p>{session?.artists?.join(" / ") || "MusicWeb"}</p><small>{session?.album || session?.platform || ""}</small></div>
        <span className="qualityBadge">{session?.quality || "AUTO"}</span>
      </div>

      {!ready && <div className={`prepareCard ${session?.status === "failed" ? "failed" : ""}`}><strong>{session?.status === "failed" ? "准备失败" : "正在准备音频"}</strong><span>{session?.error || `${session?.progress || 0}% · 完成后将自动载入逐字歌词`}</span><i><b style={{ width: `${session?.progress || 0}%` }}/></i></div>}

      {ready && <>
        <audio ref={audio} src={session.audio_url} preload="metadata" onLoadedMetadata={(event) => { const nextDuration = event.currentTarget.duration * 1000; if (Number.isFinite(nextDuration)) setDuration(nextDuration); event.currentTarget.volume = volume; }} onPlay={() => setPlaying(true)} onPause={() => { setPlaying(false); setTime(Math.round((audio.current?.currentTime || 0) * 1000)); }} onEnded={() => setPlaying(false)} onTimeUpdate={(event) => { if (event.currentTarget.paused) setTime(Math.round(event.currentTarget.currentTime * 1000)); }}/>
        <div className="timeline"><input aria-label="播放进度" type="range" min={0} max={Math.max(effectiveDuration, 1)} step={250} value={Math.min(time, effectiveDuration || time)} onChange={(event) => seek(Number(event.target.value))}/><div><span>{formatTime(time)}</span><span>-{formatTime(Math.max(effectiveDuration - time, 0))}</span></div></div>
        <div className="transport">
          <button onClick={() => seek(time - 15000)} aria-label="后退十五秒">−15</button>
          <button className="playButton" onClick={() => void togglePlay()} aria-label={playing ? "暂停" : "播放"}>{playing ? "Ⅱ" : "▶"}</button>
          <button onClick={() => seek(time + 15000)} aria-label="前进十五秒">+15</button>
        </div>
        <div className="volumeRow"><button onClick={() => { const next = !muted; setMuted(next); if (audio.current) audio.current.muted = next; }} aria-label="静音">{muted ? "🔇" : "🔊"}</button><input aria-label="音量" type="range" min={0} max={1} step={0.01} value={muted ? 0 : volume} onChange={(event) => updateVolume(Number(event.target.value))}/></div>
      </>}
    </section>

    <section className="playerLyrics">
      <div className="lyricToolbar"><span>{source}</span><div><label><input type="checkbox" checked={showTranslation} onChange={(event) => setShowTranslation(event.target.checked)}/>译文</label><label><input type="checkbox" checked={showRoma} onChange={(event) => setShowRoma(event.target.checked)}/>罗马音</label></div></div>
      <div className="lyricCanvas">
        {visibleLines.length ? <LyricPlayer lyricLines={visibleLines} currentTime={time} playing={playing} enableSpring={!lowPower} enableBlur={!lowPower} enableScale={!lowPower} alignAnchor="center" onLyricLineClick={(event) => seek(event.line.getLine().startTime)}/> : <div className="lyricEmpty"><b>{ready ? "正在加载歌词" : "音乐准备中"}</b><span>{source}</span></div>}
      </div>
    </section>
  </main>;
}
