package server

// glassCSS is the shared "liquid glass" design system for all embedded pages.
// It is deliberately CSS-only: translucent surfaces with backdrop-filter blur
// and saturation, specular rim highlights, and a fixed ambient gradient
// backdrop. backdrop-filter is applied only to a handful of top-level surfaces
// (never per-row) so pages stay cheap to composite on low-end devices, and a
// @supports fallback keeps everything readable where backdrop-filter is
// unavailable.
const glassCSS = `
    :root {
      color-scheme: light dark;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
      --text: #0f172a;
      --muted: #55607a;
      --accent: #2563eb;
      --accent-2: #7c3aed;
      --danger: #dc2626;
      --ok: #16a34a;
      --bg-base: #eef1f8;
      --blob-a: rgba(96,165,250,.50);
      --blob-b: rgba(167,139,250,.42);
      --blob-c: rgba(244,114,182,.30);
      --blob-d: rgba(45,212,191,.32);
      --glass-bg: rgba(255,255,255,.52);
      --glass-bg-strong: rgba(255,255,255,.66);
      --glass-border: rgba(255,255,255,.65);
      --glass-inner: rgba(255,255,255,.80);
      --glass-shadow: 0 18px 40px rgba(30,41,59,.16);
      --field-bg: rgba(255,255,255,.62);
      --field-border: rgba(15,23,42,.14);
      --hairline: rgba(15,23,42,.10);
      --row-hover: rgba(255,255,255,.45);
      --chip-bg: rgba(255,255,255,.55);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --text: #e6eaf2;
        --muted: #9aa7bf;
        --accent: #60a5fa;
        --accent-2: #a78bfa;
        --bg-base: #0a0e1a;
        --blob-a: rgba(37,99,235,.34);
        --blob-b: rgba(124,58,237,.30);
        --blob-c: rgba(219,39,119,.20);
        --blob-d: rgba(13,148,136,.22);
        --glass-bg: rgba(22,28,45,.52);
        --glass-bg-strong: rgba(22,28,45,.70);
        --glass-border: rgba(255,255,255,.14);
        --glass-inner: rgba(255,255,255,.22);
        --glass-shadow: 0 18px 40px rgba(0,0,0,.45);
        --field-bg: rgba(10,14,26,.55);
        --field-border: rgba(255,255,255,.16);
        --hairline: rgba(255,255,255,.09);
        --row-hover: rgba(255,255,255,.055);
        --chip-bg: rgba(255,255,255,.08);
      }
    }
    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg-base); color: var(--text); }
    /* Fixed ambient backdrop the glass refracts; a single cached paint layer. */
    body::before {
      content: ""; position: fixed; inset: 0; z-index: -1;
      background:
        radial-gradient(42rem 42rem at 12% -8%, var(--blob-a), transparent 60%),
        radial-gradient(38rem 38rem at 96% 12%, var(--blob-b), transparent 62%),
        radial-gradient(34rem 34rem at 78% 96%, var(--blob-c), transparent 60%),
        radial-gradient(30rem 30rem at -6% 78%, var(--blob-d), transparent 60%);
    }
    .glass {
      background: var(--glass-bg);
      -webkit-backdrop-filter: blur(20px) saturate(1.7);
      backdrop-filter: blur(20px) saturate(1.7);
      border: 1px solid var(--glass-border);
      border-radius: 24px;
      box-shadow: var(--glass-shadow), inset 0 1px 0 var(--glass-inner), inset 0 -1px 0 rgba(255,255,255,.06);
    }
    @supports not ((backdrop-filter: blur(1px)) or (-webkit-backdrop-filter: blur(1px))) {
      .glass { background: var(--glass-bg-strong); }
    }
    select, input, textarea {
      border: 1px solid var(--field-border); border-radius: 12px; padding: 12px 14px;
      font-size: 15px; background: var(--field-bg); color: var(--text);
    }
    input:focus, select:focus, textarea:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
    button {
      border: 1px solid transparent; border-radius: 12px; padding: 12px 14px; font-size: 15px;
      cursor: pointer; color: white; font-weight: 650;
      background: linear-gradient(180deg, color-mix(in srgb, var(--accent) 92%, white), var(--accent));
      box-shadow: inset 0 1px 0 rgba(255,255,255,.35), 0 6px 18px color-mix(in srgb, var(--accent) 35%, transparent);
      transition: filter .15s ease, transform .05s ease;
    }
    button:hover { filter: brightness(1.07); }
    button:active { transform: translateY(1px); }
    button:disabled, textarea:disabled, input:disabled { opacity: .5; cursor: not-allowed; }
    button.secondary {
      background: var(--chip-bg); color: var(--accent);
      border-color: var(--field-border); box-shadow: inset 0 1px 0 var(--glass-inner);
    }
    button.danger {
      background: linear-gradient(180deg, color-mix(in srgb, var(--danger) 92%, white), var(--danger));
      box-shadow: inset 0 1px 0 rgba(255,255,255,.3), 0 6px 18px color-mix(in srgb, var(--danger) 30%, transparent);
    }
    a { color: var(--accent); text-decoration: none; font-weight: 650; }
`

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <meta name="color-scheme" content="light dark" />
  <title>MusicBot-Go Web</title>
  <style>` + glassCSS + `
    .wrap { max-width: 1080px; margin: 0 auto; padding: 40px 20px 64px; }
    .hero { padding: 34px 30px; position: relative; overflow: hidden; }
    .hero::after {
      content: ""; position: absolute; right: -70px; top: -70px; width: 240px; height: 240px;
      background: radial-gradient(closest-side, color-mix(in srgb, var(--accent) 28%, transparent), transparent);
      pointer-events: none;
    }
    h1 { margin: 0 0 8px; font-size: 34px; letter-spacing: -0.5px; }
    h1 .dot { background: linear-gradient(90deg, var(--accent), var(--accent-2)); -webkit-background-clip: text; background-clip: text; color: transparent; }
    .sub { color: var(--muted); margin: 0; }
    .search { display: grid; grid-template-columns: 220px 1fr 120px; gap: 12px; margin-top: 24px; }
    .hint { margin: 10px 2px 0; font-size: 12.5px; color: var(--muted); }
    .panel { margin-top: 22px; padding: 18px; }
    .row { display: grid; grid-template-columns: 72px 1fr auto; gap: 14px; align-items: center; padding: 14px; border-bottom: 1px solid var(--hairline); border-radius: 14px; transition: background .15s ease; }
    .row:hover { background: var(--row-hover); }
    .row:last-child { border-bottom: none; }
    .cover { width: 64px; height: 64px; border-radius: 14px; background: var(--hairline); object-fit: cover; box-shadow: 0 4px 12px rgba(15,23,42,.15); }
    .title { font-weight: 750; margin-bottom: 5px; }
    .meta { color: var(--muted); font-size: 13px; }
    .actions { display: flex; gap: 8px; align-items: center; justify-content: flex-end; flex-wrap: wrap; }
    .actions select, .lyric-actions select { padding: 9px 10px; font-size: 13.5px; }
    .actions button { padding: 9px 12px; font-size: 13.5px; }
    .lyric-actions { display: flex; gap: 7px; align-items: center; flex-wrap: wrap; }
    .lyric-actions select { max-width: 110px; }
    .lyric-toggle { color: var(--muted); font-size: 12px; white-space: nowrap; }
    .lyric-toggle input { width: auto; padding: 0; vertical-align: middle; }
    .msg { color: var(--muted); padding: 16px; }
    .downloads-panel { display: none; }
    .panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 8px; }
    .panel-head h2 { margin: 0; font-size: 20px; }
    .job { margin-top: 12px; padding: 12px 14px; border-radius: 16px; background: var(--chip-bg); display: grid; grid-template-columns: 1fr auto; gap: 12px; align-items: center; border: 1px solid var(--hairline); box-shadow: inset 0 1px 0 var(--glass-inner); }
    .job-title { font-weight: 700; margin-bottom: 4px; }
    .job-actions { display: flex; gap: 10px; align-items: center; }
    .progress { height: 8px; background: var(--hairline); border-radius: 999px; overflow: hidden; margin-top: 8px; }
    .progress > span { display: block; height: 100%; background: linear-gradient(90deg, var(--accent), var(--accent-2)); width: 0%; transition: width .2s ease; }
    .toast { position: fixed; right: 24px; bottom: 24px; z-index: 50; padding: 13px 16px; border-radius: 16px; display: none; max-width: 360px; }
    .toast button { margin-left: 10px; padding: 6px 10px; font-size: 13px; border-radius: 9px; }
    footer { margin-top: 28px; text-align: center; color: var(--muted); font-size: 12.5px; }
    @media (max-width: 760px) {
      .wrap { padding: 20px 14px 48px; }
      .hero { padding: 24px 18px; }
      h1 { font-size: 27px; }
      .search { grid-template-columns: 1fr; }
      .row { grid-template-columns: 56px 1fr; }
      .actions { grid-column: 1 / -1; justify-content: flex-start; }
      .cover { width: 52px; height: 52px; }
      .job { grid-template-columns: 1fr; }
      .toast { left: 14px; right: 14px; max-width: none; }
    }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="hero glass">
      <h1>MusicBot-Go <span class="dot">Web</span></h1>
      <p class="sub">搜索歌曲或直接粘贴歌曲链接，选择音质并下载音乐、歌词与封面。</p>
      <div class="search">
        <select id="platform" aria-label="选择平台"></select>
        <input id="query" placeholder="输入歌曲名 / 歌手，或直接粘贴歌曲链接" autocomplete="off" />
        <button id="searchBtn">搜索</button>
      </div>
      <p class="hint">支持网易云 / QQ 音乐 / 酷狗 / 汽水 / 哔哩哔哩 / Apple Music 等；粘贴链接会自动识别平台并解析。</p>
    </section>
    <section id="downloadsPanel" class="panel glass downloads-panel">
      <div class="panel-head">
        <div>
          <h2>下载任务</h2>
          <div class="meta">真正开始下载的歌曲会集中显示在这里。</div>
        </div>
        <button id="clearDoneBtn" class="secondary">清除已完成</button>
      </div>
      <div id="jobs"></div>
    </section>
    <section class="panel glass">
      <div id="status" class="msg">正在加载平台列表...</div>
      <div id="results"></div>
    </section>
    <footer id="footer"></footer>
    <div id="toast" class="toast glass" role="status" aria-live="polite"></div>
  </main>
  <script>
    const $ = (id) => document.getElementById(id);
    const platformSelect = $("platform");
    const results = $("results");
    const jobs = $("jobs");
    const status = $("status");
    const downloadsPanel = $("downloadsPanel");
    const toast = $("toast");
    const lyricFormats = [
      ["lrc", "LRC（逐行）"], ["yrc", "YRC（逐词）"], ["qrc", "QRC（逐词）"],
      ["lys", "LYS（逐词）"], ["krc", "KRC"], ["elrc", "ELRC"], ["spl", "SPL"],
      ["ass", "ASS 字幕"], ["lqe", "LQE"], ["ttml", "TTML"], ["amjson", "Apple JSON"],
      ["srt", "SRT 字幕"], ["txt", "TXT 纯文本"], ["trans", "仅翻译"], ["roma", "仅罗马音"]
    ];

    async function api(url, opts) {
      const res = await fetch(url, opts);
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || res.statusText);
      return data;
    }

    async function loadPlatforms() {
      const data = await api("/api/platforms");
      platformSelect.innerHTML = "";
      for (const p of data.platforms || []) {
        if (!p.capabilities || !p.capabilities.search) continue;
        const opt = document.createElement("option");
        opt.value = p.name;
        opt.textContent = (p.emoji || "🎵") + " " + (p.display_name || p.name);
        platformSelect.appendChild(opt);
      }
      status.textContent = "请输入关键词或粘贴歌曲链接开始。";
    }

    async function loadHealth() {
      try {
        const d = await api("/api/v1/health");
        const hours = Math.floor((d.uptime_seconds || 0) / 3600);
        $("footer").textContent = "服务运行中 · 已接入 " + (d.platforms || 0) + " 个平台 · 已运行 " + hours + " 小时";
      } catch (e) { /* footer is decorative */ }
    }

    function artistText(item) {
      return (item.artists || []).join(" / ") || "未知艺人";
    }

    function looksLikeLink(text) {
      return /^https?:\/\/\S+$/i.test(text.trim());
    }

    function showToast(message, actionText, action) {
      toast.innerHTML = "<span></span>";
      toast.querySelector("span").textContent = message;
      if (actionText && action) {
        const btn = document.createElement("button");
        btn.className = "secondary";
        btn.textContent = actionText;
        btn.onclick = action;
        toast.appendChild(btn);
      }
      toast.style.display = "block";
      clearTimeout(showToast.timer);
      showToast.timer = setTimeout(() => toast.style.display = "none", 4200);
    }

    function showDownloads(focus) {
      downloadsPanel.style.display = "block";
      if (focus) downloadsPanel.scrollIntoView({behavior: "smooth", block: "start"});
    }

    function makeCover(item) {
      const img = item.cover_url || item.coverUrl || item.cover || "";
      if (!img) {
        const blank = document.createElement("div");
        blank.className = "cover";
        return blank;
      }
      const cover = document.createElement("img");
      cover.className = "cover";
      cover.src = img;
      cover.loading = "lazy";
      cover.referrerPolicy = "no-referrer";
      cover.onerror = () => {
        const blank = document.createElement("div");
        blank.className = "cover";
        cover.replaceWith(blank);
      };
      return cover;
    }

    function renderResults(items, message) {
      results.innerHTML = "";
      if (!items.length) {
        status.textContent = "没有搜索结果。";
        return;
      }
      status.textContent = message || "共 " + items.length + " 条结果。";
      for (const item of items) {
        const row = document.createElement("div");
        row.className = "row";
        const info = document.createElement("div");
        const title = document.createElement("div");
        title.className = "title";
        title.textContent = item.title || item.track_id;
        const artist = document.createElement("div");
        artist.className = "meta";
        artist.textContent = artistText(item);
        const album = document.createElement("div");
        album.className = "meta album";
        album.textContent = item.album ? "专辑：" + item.album : "专辑：未知";
        info.append(title, artist, album);

        const actions = document.createElement("div");
        actions.className = "actions";
        const quality = document.createElement("select");
        quality.className = "quality";
        for (const option of item.qualities || []) {
          const node = document.createElement("option");
          node.value = option.value;
          node.textContent = option.label;
          quality.appendChild(node);
        }
        const download = document.createElement("button");
        download.className = "secondary";
        download.textContent = "下载";
        download.onclick = () => createDownload(item, quality.value, download);

        const lyricActions = document.createElement("div");
        lyricActions.className = "lyric-actions";
        const lyricFormat = document.createElement("select");
        for (const pair of lyricFormats) {
          const option = document.createElement("option");
          option.value = pair[0];
          option.textContent = pair[1];
          lyricFormat.appendChild(option);
        }
        const translation = lyricToggle("翻译");
        const roma = lyricToggle("罗马音");
        const lyricButton = document.createElement("button");
        lyricButton.className = "secondary";
        lyricButton.textContent = "下载歌词";
        lyricButton.onclick = () => downloadLyrics(item, lyricFormat.value, translation.input.checked, roma.input.checked, lyricButton);
        lyricActions.append(lyricFormat, translation.label, roma.label, lyricButton);
        actions.append(quality, download, lyricActions);
        row.append(makeCover(item), info, actions);
        results.appendChild(row);
      }
    }

    function lyricToggle(text) {
      const label = document.createElement("label");
      label.className = "lyric-toggle";
      const input = document.createElement("input");
      input.type = "checkbox";
      label.append(input, document.createTextNode(" " + text));
      return {label, input};
    }

    async function search() {
      const q = $("query").value.trim();
      if (!q) return;
      if (looksLikeLink(q)) {
        return parseLink(q);
      }
      const platform = platformSelect.value;
      status.textContent = "搜索中...";
      results.innerHTML = "";
      try {
        const data = await api("/api/search?platform=" + encodeURIComponent(platform) + "&q=" + encodeURIComponent(q) + "&limit=20");
        renderResults(data.results || []);
      } catch (e) {
        status.textContent = "搜索失败：" + e.message;
      }
    }

    async function parseLink(link) {
      const button = $("searchBtn");
      button.disabled = true;
      button.textContent = "解析中...";
      status.textContent = "检测到链接，正在解析...";
      results.innerHTML = "";
      try {
        const data = await api("/api/parse?url=" + encodeURIComponent(link));
        renderResults(data.result ? [data.result] : [], "链接解析成功：以下为该链接对应的歌曲。");
      } catch (e) {
        status.textContent = "链接解析失败：" + e.message;
      } finally {
        button.disabled = false;
        button.textContent = "搜索";
      }
    }

    async function createDownload(item, quality, button) {
      showDownloads(true);
      if (button) {
        button.disabled = true;
        button.textContent = "加入中...";
      }
      const box = document.createElement("div");
      box.className = "job";
      box.dataset.done = "false";
      box.innerHTML = '<div><div class="job-title"></div><div class="meta">正在创建下载任务...</div><div class="progress"><span></span></div></div><div class="job-actions"></div>';
      box.querySelector(".job-title").textContent = item.title || item.track_id;
      jobs.prepend(box);
      try {
        const job = await api("/api/downloads", {
          method: "POST",
          headers: {"Content-Type": "application/json"},
          body: JSON.stringify({platform: item.platform, track_id: item.track_id, quality})
        });
        renderJob(job, job.job_id, box);
        pollJob(job.job_id, box);
        showToast("已加入下载任务（" + (job.quality || quality) + "）：" + (item.title || item.track_id), "查看", () => showDownloads(true));
      } catch (e) {
        box.dataset.done = "true";
        box.querySelector(".meta").textContent = "创建失败：" + e.message;
        box.querySelector(".progress > span").style.width = "100%";
        showToast("创建下载任务失败：" + e.message);
        return;
      } finally {
        // 下载任务的去重由后端按“平台 + 歌曲 + 音质”处理。前端只在请求
        // 进行时锁定按钮，因此切换到另一种音质后可立即创建独立任务。
        if (button) {
          button.disabled = false;
          button.textContent = "下载";
        }
      }
    }

    async function downloadLyrics(item, format, translation, roma, button) {
      if (!item.platform || !item.track_id) {
        showToast("该歌曲没有可用的平台或歌曲 ID。");
        return;
      }
      button.disabled = true;
      button.textContent = "准备歌词...";
      try {
        const endpoint = "/api/lyrics/file?platform=" + encodeURIComponent(item.platform)
          + "&track_id=" + encodeURIComponent(item.track_id)
          + "&format=" + encodeURIComponent(format)
          + "&translation=" + (translation ? "1" : "0")
          + "&roma=" + (roma ? "1" : "0");
        const response = await fetch(endpoint);
        if (!response.ok) {
          const data = await response.json().catch(() => ({}));
          throw new Error(data.error || response.statusText);
        }
        const blob = await response.blob();
        const link = document.createElement("a");
        link.href = URL.createObjectURL(blob);
        link.download = (item.title || item.track_id) + "." + lyricExtension(format);
        document.body.appendChild(link);
        link.click();
        link.remove();
        setTimeout(() => URL.revokeObjectURL(link.href), 1000);
        showToast("歌词文件已开始下载：" + (item.title || item.track_id));
      } catch (e) {
        showToast("歌词下载失败：" + e.message);
      } finally {
        button.disabled = false;
        button.textContent = "下载歌词";
      }
    }

    function lyricExtension(format) {
      if (format === "amjson") return "json";
      if (format === "ttml") return "ttml";
      if (format === "ass") return "ass";
      if (format === "srt") return "srt";
      if (format === "txt" || format === "trans" || format === "roma") return "txt";
      if (format === "elrc") return "lrc";
      return format || "lrc";
    }

    function renderJob(job, id, box) {
      const title = job.title || job.track_id || "下载任务";
      const artists = (job.artists || []).join(" / ");
      const pct = Math.max(0, Math.min(100, job.progress || 0));
      box.querySelector(".job-title").textContent = title;
      box.querySelector(".meta").textContent = (artists ? artists + " · " : "") + (job.quality || "") + " · " + job.status + " · " + pct + "%";
      box.querySelector(".progress > span").style.width = pct + "%";
      const actions = box.querySelector(".job-actions");
      actions.innerHTML = "";
      if (job.status === "ready") {
        box.dataset.done = "true";
        box.querySelector(".progress > span").style.width = "100%";
        const a = document.createElement("a");
        a.href = "/api/downloads/" + encodeURIComponent(id) + "/file";
        a.textContent = "下载文件";
        actions.appendChild(a);
        showToast("下载已准备好：" + title, "下载", () => { window.location.href = a.href; });
        return true;
      }
      if (job.status === "failed") {
        box.dataset.done = "true";
        const err = document.createElement("span");
        err.style.color = "var(--danger)";
        err.textContent = job.error || "失败";
        actions.appendChild(err);
        showToast("下载失败：" + (job.error || title));
        return true;
      }
      return false;
    }

    async function pollJob(id, box) {
      if (window.EventSource) {
        const es = new EventSource("/api/downloads/" + encodeURIComponent(id) + "/events");
        es.addEventListener("job", (ev) => {
          const job = JSON.parse(ev.data);
          if (renderJob(job, id, box)) es.close();
        });
        es.addEventListener("error", () => {
          es.close();
          pollJobFallback(id, box);
        });
        return;
      }
      pollJobFallback(id, box);
    }

    async function pollJobFallback(id, box) {
      try {
        const job = await api("/api/downloads/" + encodeURIComponent(id));
        if (renderJob(job, id, box)) return;
        setTimeout(() => pollJobFallback(id, box), 1200);
      } catch (e) {
        box.textContent = "任务查询失败：" + e.message;
      }
    }

    $("clearDoneBtn").onclick = () => {
      for (const node of Array.from(jobs.children)) {
        if (node.dataset.done === "true") node.remove();
      }
      if (!jobs.children.length) downloadsPanel.style.display = "none";
    };
    $("searchBtn").onclick = search;
    $("query").addEventListener("keydown", (e) => { if (e.key === "Enter") search(); });
    $("query").addEventListener("paste", (e) => {
      const text = (e.clipboardData || window.clipboardData).getData("text") || "";
      if (looksLikeLink(text)) {
        e.preventDefault();
        $("query").value = text.trim();
        parseLink(text.trim());
      }
    });
    loadPlatforms().catch(e => status.textContent = "平台加载失败：" + e.message);
    loadHealth();
  </script>
</body>
</html>`
