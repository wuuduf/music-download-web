<template>
  <a v-if="projectId" class="studio-mode-switch" :href="`/studio/${encodeURIComponent(projectId)}`">
    切换到 TTML Tool
  </a>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import { useToast } from 'primevue/usetoast'

import { audioEngine } from '@core/audio'
import { parseTTML, stringifyTTML } from '@core/convert/formats/ttml'
import { applyPersist, collectPersist } from '@states/services/port'
import { useCoreStore } from '@states/stores'

interface ProjectMetadata {
  music_name: string
  music_names?: string[]
  artists?: string[]
  album?: string
  albums?: string[]
  isrc?: string
  isrcs?: string[]
  external_ids?: Record<string, string[]>
}

interface Bootstrap {
  project: { metadata?: ProjectMetadata }
  audio_url: string
  seed_lyric_url: string
  revision: number
}

const platformMetadataKeys: Record<string, string> = {
  netease: 'ncmMusicId',
  qqmusic: 'qqMusicId',
  spotify: 'spotifyId',
  applemusic: 'appleMusicId',
}

const match = location.pathname.match(/^\/studio-editor\/([^/]+)/)
const projectId = match ? decodeURIComponent(match[1]!) : ''
const toast = useToast()
const coreStore = useCoreStore()
let revision = 0
let initialized = false
let saveTimer: number | undefined

async function checkedFetch(url: string, init?: RequestInit) {
  const response = await fetch(url, init)
  if (response.status === 401) {
    location.href = `/admin/login?next=${encodeURIComponent(location.pathname)}`
    throw new Error('需要管理员登录')
  }
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(data.error || `HTTP ${response.status}`)
  }
  return response
}

async function fetchAudio(url: string) {
  for (let attempt = 0; attempt < 180; attempt += 1) {
    const response = await fetch(url)
    if (response.ok) return response
    if (response.status !== 409) throw new Error(`音频加载失败：HTTP ${response.status}`)
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  throw new Error('音频准备超时')
}

function unique(values: Array<string | undefined>) {
  return [...new Set(values.map((value) => value?.trim()).filter(Boolean) as string[])]
}

function mergeMetadata(target: Record<string, string[]>, metadata?: ProjectMetadata) {
  if (!metadata) return
  const incoming: Record<string, string[]> = {
    musicName: unique([metadata.music_name, ...(metadata.music_names ?? [])]),
    artists: unique(metadata.artists ?? []),
    album: unique([metadata.album, ...(metadata.albums ?? [])]),
    isrc: unique([metadata.isrc, ...(metadata.isrcs ?? [])]),
  }
  for (const [platform, ids] of Object.entries(metadata.external_ids ?? {})) {
    const key = platformMetadataKeys[platform]
    if (key) incoming[key] = unique(ids)
  }
  for (const [key, values] of Object.entries(incoming)) {
    if (!values.length) continue
    target[key] = unique([...(target[key] ?? []), ...values])
  }
}

async function bootstrap() {
  if (!projectId) return
  const data = (await checkedFetch(
    `/api/v1/studio/projects/${encodeURIComponent(projectId)}/bootstrap`,
  ).then((response) => response.json())) as Bootstrap
  revision = data.revision

  const lyricText = await checkedFetch(data.seed_lyric_url).then((response) => response.text())
  const persist = parseTTML(lyricText)
  mergeMetadata(persist.metadata, data.project.metadata)
  applyPersist(persist)

  const audioResponse = await fetchAudio(data.audio_url)
  const blob = await audioResponse.blob()
  const ext = blob.type.includes('flac') ? 'flac' : blob.type.includes('mp4') ? 'm4a' : 'mp3'
  audioEngine.mount(new File([blob], `music.${ext}`, { type: blob.type }))
  initialized = true
  toast.add({
    severity: 'success',
    summary: 'MusicWeb 项目已导入',
    detail: '音频、逐字歌词和跨平台元数据已加载',
    life: 4000,
  })
}

async function saveRevision() {
  if (!projectId || !initialized) return
  const persist = collectPersist()
  const response = await checkedFetch(
    `/api/v1/studio/projects/${encodeURIComponent(projectId)}/revisions`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        expected_revision: revision,
        content: stringifyTTML(persist),
        metadata: persist.metadata,
      }),
    },
  )
  const data = (await response.json()) as { revision: number }
  revision = data.revision
}

const stopAutosave = watch(
  [() => coreStore.lyricLines, () => coreStore.metadata],
  () => {
    if (!initialized) return
    window.clearTimeout(saveTimer)
    saveTimer = window.setTimeout(() => {
      void saveRevision().catch((error: Error) => {
        toast.add({
          severity: String(error.message).includes('冲突') ? 'warn' : 'error',
          summary: '服务端自动保存失败',
          detail: error.message,
          life: 5000,
        })
      })
    }, 2000)
  },
  { deep: true },
)

onMounted(() => {
  void bootstrap().catch((error: Error) => {
    toast.add({ severity: 'error', summary: '项目导入失败', detail: error.message, life: 6000 })
  })
})

onUnmounted(() => {
  stopAutosave()
  window.clearTimeout(saveTimer)
})
</script>

<style scoped>
.studio-mode-switch {
  position: fixed;
  z-index: 10000;
  top: 10px;
  right: 14px;
  padding: 8px 12px;
  border: 1px solid color-mix(in srgb, var(--p-primary-color), transparent 55%);
  border-radius: 999px;
  color: var(--p-primary-color);
  background: color-mix(in srgb, var(--p-content-background), transparent 8%);
  box-shadow: 0 6px 20px rgb(0 0 0 / 12%);
  text-decoration: none;
  font-size: 12px;
  font-weight: 700;
  backdrop-filter: blur(12px);
}
</style>
