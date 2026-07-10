package server

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>MusicBot-Go Web</title>
  <style>
    :root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f6f7fb; color: #111827; }
    .wrap { max-width: 1080px; margin: 0 auto; padding: 40px 20px; }
    .hero { background: linear-gradient(135deg,#111827,#374151); color: white; padding: 30px; border-radius: 24px; box-shadow: 0 20px 50px rgba(17,24,39,.18); }
    h1 { margin: 0 0 8px; font-size: 34px; }
    .sub { opacity: .82; margin: 0; }
    .search { display: grid; grid-template-columns: 220px 1fr 120px; gap: 12px; margin-top: 24px; }
    .link-mode { margin-top: 16px; padding-top: 16px; border-top: 1px solid rgba(255,255,255,.16); }
    .link-mode-title { margin: 0 0 8px; font-size: 14px; font-weight: 700; opacity: .9; }
    .link-search { display: grid; grid-template-columns: 1fr 120px; gap: 12px; }
    select,input,button { border: 1px solid #d1d5db; border-radius: 12px; padding: 12px 14px; font-size: 15px; }
    button { cursor: pointer; background: #2563eb; border-color: #2563eb; color: white; font-weight: 650; }
    button.secondary { background: #eef2ff; color: #1d4ed8; border-color: #c7d2fe; }
    .panel { background: white; margin-top: 22px; border-radius: 20px; padding: 18px; box-shadow: 0 10px 30px rgba(15,23,42,.08); }
    .row { display: grid; grid-template-columns: 72px 1fr auto; gap: 14px; align-items: center; padding: 14px; border-bottom: 1px solid #edf0f5; }
    .row:last-child { border-bottom: none; }
    .cover { width: 64px; height: 64px; border-radius: 12px; background: #e5e7eb; object-fit: cover; }
    .title { font-weight: 750; margin-bottom: 5px; }
    .meta { color: #6b7280; font-size: 13px; }
    .actions { display: flex; gap: 8px; align-items: center; justify-content: flex-end; flex-wrap: wrap; }
    .lyric-actions { display: flex; gap: 7px; align-items: center; flex-wrap: wrap; }
    .lyric-actions select { max-width: 110px; }
    .lyric-toggle { color: #4b5563; font-size: 12px; white-space: nowrap; }
    .lyric-toggle input { width: auto; padding: 0; vertical-align: middle; }
    .msg { color: #6b7280; padding: 16px; }
    .downloads-panel { display: none; border: 1px solid #dbeafe; }
    .panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 8px; }
    .panel-head h2 { margin: 0; font-size: 20px; }
    .job { margin-top: 12px; padding: 12px 14px; border-radius: 14px; background: #f9fafb; display: grid; grid-template-columns: 1fr auto; gap: 12px; align-items: center; border: 1px solid #eef2ff; }
    .job-title { font-weight: 700; margin-bottom: 4px; }
    .job-actions { display: flex; gap: 10px; align-items: center; }
    .progress { height: 8px; background: #e5e7eb; border-radius: 999px; overflow: hidden; margin-top: 8px; }
    .progress > span { display: block; height: 100%; background: #2563eb; width: 0%; transition: width .2s ease; }
    .toast { position: fixed; right: 24px; bottom: 24px; z-index: 50; background: #111827; color: white; padding: 13px 16px; border-radius: 14px; box-shadow: 0 16px 40px rgba(0,0,0,.22); display: none; max-width: 360px; }
    .toast button { margin-left: 10px; padding: 6px 10px; font-size: 13px; border-radius: 8px; }
    a { color: #2563eb; text-decoration: none; font-weight: 650; }
    @media (max-width: 760px) { .search, .link-search { grid-template-columns: 1fr; } .row { grid-template-columns: 56px 1fr; } .actions { grid-column: 1 / -1; justify-content: flex-start; } .cover { width: 52px; height: 52px; } .job { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="hero">
      <h1>MusicBot-Go Web</h1>
      <p class="sub">选择平台，搜索歌曲，选择音质并下载。第一版 Web MVP。</p>
      <div class="search">
        <select id="platform"></select>
        <input id="query" placeholder="输入歌曲名 / 歌手 / 关键词" />
        <button id="searchBtn">搜索</button>
      </div>
      <div class="link-mode">
        <p class="link-mode-title">链接解析（自动识别平台，仅返回该链接对应的一首歌）</p>
        <div class="link-search">
          <input id="linkInput" placeholder="粘贴网易云 / QQ 音乐 / Apple Music / Spotify / YouTube Music 等歌曲链接" />
          <button id="parseBtn" class="secondary">解析链接</button>
        </div>
      </div>
    </section>
    <section id="downloadsPanel" class="panel downloads-panel">
      <div class="panel-head">
        <div>
          <h2>下载任务</h2>
          <div class="meta">真正开始下载的歌曲会集中显示在这里。</div>
        </div>
        <button id="clearDoneBtn" class="secondary">清除已完成</button>
      </div>
      <div id="jobs"></div>
    </section>
    <section class="panel">
      <div id="status" class="msg">正在加载平台列表...</div>
      <div id="results"></div>
    </section>
    <div id="toast" class="toast"></div>
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
      status.textContent = "请输入关键词开始搜索。";
    }

    function artistText(item) {
      return (item.artists || []).join(" / ") || "未知艺人";
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
      const platform = platformSelect.value;
      if (!q) return;
      status.textContent = "搜索中...";
      results.innerHTML = "";
      try {
        const data = await api("/api/search?platform=" + encodeURIComponent(platform) + "&q=" + encodeURIComponent(q) + "&limit=20");
        renderResults(data.results || []);
      } catch (e) {
        status.textContent = "搜索失败：" + e.message;
      }
    }

    async function parseLink() {
      const link = $("linkInput").value.trim();
      if (!link) return;
      const button = $("parseBtn");
      button.disabled = true;
      button.textContent = "解析中...";
      status.textContent = "正在解析链接...";
      results.innerHTML = "";
      try {
        const data = await api("/api/parse?url=" + encodeURIComponent(link));
        renderResults(data.result ? [data.result] : [], "链接解析成功：以下为该链接对应的歌曲。");
      } catch (e) {
        status.textContent = "链接解析失败：" + e.message;
      } finally {
        button.disabled = false;
        button.textContent = "解析链接";
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
        if (button) {
          button.disabled = false;
          button.textContent = "下载";
        }
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
        err.style.color = "#dc2626";
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
    $("parseBtn").onclick = parseLink;
    $("linkInput").addEventListener("keydown", (e) => { if (e.key === "Enter") parseLink(); });
    loadPlatforms().catch(e => status.textContent = "平台加载失败：" + e.message);
  </script>
</body>
</html>`
