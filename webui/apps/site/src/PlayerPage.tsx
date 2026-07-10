import { useEffect, useMemo, useRef, useState } from "react";
import { LyricPlayer } from "@applemusic-like-lyrics/react";
import { parseTTML } from "@applemusic-like-lyrics/lyric";
import type { LyricLine } from "@applemusic-like-lyrics/core";
import { api, coverURL, type LyricInfo, type PlaybackSession } from "./api";

export function PlayerPage({ sessionId }: { sessionId: string }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [session, setSession] = useState<PlaybackSession>();
  const [lines, setLines] = useState<LyricLine[]>([]);
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [showTranslation, setShowTranslation] = useState(true);
  const [showRoma, setShowRoma] = useState(true);
  const [lowPower, setLowPower] = useState(false);
  const [source, setSource] = useState("正在准备歌词…");

  useEffect(() => {
    let cancelled = false;
    async function load() {
      const current = await api<PlaybackSession>(`/api/v1/playback/sessions/${encodeURIComponent(sessionId)}`);
      if (cancelled) return;
      setSession(current);
      if (current.status !== "ready") { setTimeout(load, 1000); return; }
      const lyric = await api<LyricInfo>(current.lyric_url);
      if (cancelled) return;
      setSource(`${lyric.source} · ${lyric.match_type} · ${lyric.confidence}%${lyric.author ? ` · ${lyric.author}` : ""}`);
      setLines(parseTTML(lyric.content).lines);
    }
    load().catch((error) => setSource(`加载失败：${error.message}`));
    return () => { cancelled = true; };
  }, [sessionId]);

  useEffect(() => {
    let frame = 0;
    const update = () => {
      if (audio.current && !audio.current.paused) setTime(Math.round(audio.current.currentTime * 1000));
      frame = requestAnimationFrame(update);
    };
    frame = requestAnimationFrame(update);
    return () => cancelAnimationFrame(frame);
  }, []);

  const visibleLines = useMemo(() => lines.map((line) => ({ ...line, translatedLyric: showTranslation ? line.translatedLyric : "", romanLyric: showRoma ? line.romanLyric : "", words: line.words.map((word) => ({ ...word })) })), [lines, showTranslation, showRoma]);

  return <main className={`playerPage ${lowPower ? "lowPower" : ""}`}>
    <div className="playerBackdrop" style={{ backgroundImage: session?.cover_url ? `url(${coverURL(session.cover_url)})` : undefined }}/><div className="playerShade"/>
    <a className="back" href="/">← 返回搜索</a>
    <section className="nowPlaying"><div className="largeCover">{session?.cover_url && <img src={coverURL(session.cover_url)} referrerPolicy="no-referrer"/>}</div><h1>{session?.title || "正在准备音频…"}</h1><p>{session?.artists?.join(" / ")} · {session?.album}</p><small>{session?.status || "loading"} · {session?.progress || 0}%</small>
      {session?.status === "ready" && <audio ref={audio} src={session.audio_url} controls preload="metadata" onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)} onSeeked={() => setTime(Math.round((audio.current?.currentTime || 0) * 1000))}/>}<div className="playerToggles"><label><input type="checkbox" checked={showTranslation} onChange={(e) => setShowTranslation(e.target.checked)}/> 翻译</label><label><input type="checkbox" checked={showRoma} onChange={(e) => setShowRoma(e.target.checked)}/> 罗马音</label><label><input type="checkbox" checked={lowPower} onChange={(e) => setLowPower(e.target.checked)}/> 低性能模式</label></div><small>{source}</small></section>
    <section className="lyricsHost"><LyricPlayer lyricLines={visibleLines} currentTime={time} playing={playing} enableSpring={!lowPower} enableBlur={!lowPower} enableScale={!lowPower} alignAnchor="center" onLyricLineClick={(event) => { const target = event.line.getLine().startTime; if (audio.current) { audio.current.currentTime = target / 1000; setTime(target); } }}/></section>
  </main>;
}
