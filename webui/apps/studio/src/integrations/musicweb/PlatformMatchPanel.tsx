import {
	Badge,
	Button,
	Dialog,
	Flex,
	Spinner,
	Text,
	TextField,
} from "@radix-ui/themes";
import { useSetAtom } from "jotai";
import { useCallback, useEffect, useState } from "react";
import { lyricLinesAtom } from "$/states/main";
import { pushNotificationAtom } from "$/states/notifications";
import styles from "./PlatformMatchPanel.module.css";
import {
	type MusicWebProjectMetadata,
	mergeMusicWebMetadata,
	musicWebProjectID,
	platformDisplayNames,
	platformMetadataKeys,
	setPlatformMetadataID,
} from "./metadata";

interface Project {
	project_id: string;
	metadata?: MusicWebProjectMetadata;
}

interface Candidate {
	platform: string;
	track_id: string;
	title: string;
	artists?: string[];
	album?: string;
	duration_ms?: number;
	isrc?: string;
	score: number;
	reasons?: string[];
	selected: boolean;
}

const reasonLabels: Record<string, string> = {
	exact_isrc: "ISRC 精确",
	exact_platform_id: "ID 精确",
	existing_id: "已有 ID",
	authoritative_isrc: "权威 ISRC",
	manual_confirm: "手动确认",
	title: "标题",
	artist: "歌手",
	album: "专辑",
	duration_within_1_5s: "时长±1.5s",
	duration_within_2s: "时长±2s",
	duration_within_3s: "时长±3s",
	duration_within_5s: "时长±5s",
};

async function api<T>(url: string, init?: RequestInit): Promise<T> {
	const response = await fetch(url, init);
	if (response.status === 401) {
		location.href = `/admin/login?next=${encodeURIComponent(location.pathname)}`;
		throw new Error("需要管理员登录");
	}
	if (!response.ok) {
		const payload = await response.json().catch(() => ({}));
		throw new Error(payload.error || `HTTP ${response.status}`);
	}
	return response.json() as Promise<T>;
}

function formatDuration(ms?: number) {
	if (!ms || ms <= 0) return "";
	const total = Math.round(ms / 1000);
	return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

export function PlatformMatchPanel() {
	const projectID = musicWebProjectID();
	const setLyrics = useSetAtom(lyricLinesAtom);
	const notify = useSetAtom(pushNotificationAtom);
	const [open, setOpen] = useState(false);
	const [metadata, setMetadata] = useState<MusicWebProjectMetadata>();
	const [loading, setLoading] = useState(false);
	const [resolving, setResolving] = useState(false);
	const [activePlatform, setActivePlatform] = useState<string>();
	const [query, setQuery] = useState("");
	const [candidates, setCandidates] = useState<Candidate[]>();
	const [searching, setSearching] = useState(false);
	const [busyKey, setBusyKey] = useState("");

	const base = `/api/v1/studio/projects/${encodeURIComponent(projectID)}`;

	const loadProject = useCallback(async () => {
		setLoading(true);
		try {
			const project = await api<Project>(base);
			setMetadata(project.metadata);
		} catch (error) {
			notify({
				title: `读取项目元数据失败：${(error as Error).message}`,
				level: "error",
				source: "平台 ID 匹配",
			});
		} finally {
			setLoading(false);
		}
	}, [base, notify]);

	useEffect(() => {
		if (open && projectID) void loadProject();
	}, [open, projectID, loadProject]);

	const search = useCallback(
		async (platform: string, keyword: string) => {
			setSearching(true);
			setCandidates(undefined);
			try {
				const data = await api<{ candidates: Candidate[] }>(
					`${base}/metadata/candidates?platform=${encodeURIComponent(platform)}&q=${encodeURIComponent(keyword)}`,
				);
				setCandidates(data.candidates ?? []);
			} catch (error) {
				notify({
					title: `搜索候选失败：${(error as Error).message}`,
					level: "error",
					source: "平台 ID 匹配",
				});
			} finally {
				setSearching(false);
			}
		},
		[base, notify],
	);

	const openSearch = useCallback(
		(platform: string) => {
			const defaultQuery = [
				metadata?.music_name ?? "",
				metadata?.artists?.[0] ?? "",
			]
				.join(" ")
				.trim();
			setActivePlatform(platform);
			setQuery(defaultQuery);
			void search(platform, "");
		},
		[metadata, search],
	);

	const confirm = useCallback(
		async (platform: string, trackID: string) => {
			setBusyKey(`${platform}:${trackID}`);
			try {
				const project = await api<Project>(`${base}/metadata/confirm`, {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ platform, track_id: trackID }),
				});
				setMetadata(project.metadata);
				setLyrics((current) =>
					setPlatformMetadataID(
						mergeMusicWebMetadata(current, project.metadata),
						platform,
						trackID,
					),
				);
				setActivePlatform(undefined);
				setCandidates(undefined);
				notify({
					title: `已导入 ${platformDisplayNames[platform] ?? platform} ID：${trackID}`,
					level: "success",
					source: "平台 ID 匹配",
				});
			} catch (error) {
				notify({
					title: `导入失败：${(error as Error).message}`,
					level: "error",
					source: "平台 ID 匹配",
				});
			} finally {
				setBusyKey("");
			}
		},
		[base, notify, setLyrics],
	);

	const remove = useCallback(
		async (platform: string) => {
			setBusyKey(`remove:${platform}`);
			try {
				const project = await api<Project>(`${base}/metadata/confirm`, {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ platform, remove: true }),
				});
				setMetadata(project.metadata);
				setLyrics((current) => setPlatformMetadataID(current, platform, ""));
				notify({
					title: `已移除 ${platformDisplayNames[platform] ?? platform} 的 ID`,
					level: "info",
					source: "平台 ID 匹配",
				});
			} catch (error) {
				notify({
					title: `移除失败：${(error as Error).message}`,
					level: "error",
					source: "平台 ID 匹配",
				});
			} finally {
				setBusyKey("");
			}
		},
		[base, notify, setLyrics],
	);

	const resolveAll = useCallback(async () => {
		setResolving(true);
		try {
			const project = await api<Project>(`${base}/metadata/resolve`, {
				method: "POST",
			});
			setMetadata(project.metadata);
			setLyrics((current) =>
				mergeMusicWebMetadata(current, project.metadata),
			);
			const matched = Object.values(project.metadata?.external_ids ?? {}).filter(
				(ids) => ids.length > 0,
			).length;
			notify({
				title: `自动匹配完成：${matched}/4 个平台已确认`,
				level: "success",
				source: "平台 ID 匹配",
			});
		} catch (error) {
			notify({
				title: `自动匹配失败：${(error as Error).message}`,
				level: "error",
				source: "平台 ID 匹配",
			});
		} finally {
			setResolving(false);
		}
	}, [base, notify, setLyrics]);

	if (!projectID) return null;

	return (
		<Dialog.Root
			open={open}
			onOpenChange={(next) => {
				setOpen(next);
				if (!next) {
					setActivePlatform(undefined);
					setCandidates(undefined);
				}
			}}
		>
			<Dialog.Trigger>
				<Button className={styles.trigger} size="2" variant="soft">
					平台 ID
				</Button>
			</Dialog.Trigger>
			<Dialog.Content className={styles.content}>
				<Dialog.Title>跨平台 ID 匹配</Dialog.Title>
				<Dialog.Description>
					搜索四个平台的歌曲 ID 并导入 TTML 元数据（ncmMusicId / qqMusicId /
					spotifyId / appleMusicId）。
				</Dialog.Description>
				<Flex direction="column" gap="3" mt="4">
					<Flex justify="between" align="center">
						<Text size="2" color="gray">
							{loading
								? "正在读取项目元数据…"
								: metadata
									? `${metadata.music_name}${metadata.artists?.length ? ` · ${metadata.artists[0]}` : ""}`
									: "未读取到元数据"}
						</Text>
						<Button
							size="1"
							variant="soft"
							disabled={resolving || loading}
							onClick={() => void resolveAll()}
						>
							{resolving && <Spinner size="1" />}
							{resolving ? "匹配中…" : "重新自动匹配"}
						</Button>
					</Flex>
					<Flex direction="column" gap="2">
						{Object.keys(platformMetadataKeys).map((platform) => {
							const ids = metadata?.external_ids?.[platform] ?? [];
							const match = metadata?.matches?.[platform];
							const confirmed = ids.length > 0;
							return (
								<Flex
									key={platform}
									className={styles.platformRow}
									justify="between"
									align="center"
									gap="3"
								>
									<Flex direction="column" gap="1" className={styles.grow}>
										<Flex align="center" gap="2">
											<Text size="2" weight="bold">
												{platformDisplayNames[platform]}
											</Text>
											{confirmed ? (
												<Badge color="green">已匹配</Badge>
											) : match?.error ? (
												<Badge color="red">失败</Badge>
											) : (
												<Badge color="amber">待确认</Badge>
											)}
											{match && match.score > 0 && (
												<Badge color="gray" variant="soft">
													{match.score} 分
												</Badge>
											)}
										</Flex>
										<Text size="1" color="gray" className={styles.idText}>
											{confirmed
												? ids[0]
												: match?.error
													? match.error
													: match?.track_id
														? `候选：${match.track_id}（未达自动确认阈值）`
														: "尚无候选，请搜索"}
										</Text>
									</Flex>
									<Flex gap="2">
										<Button
											size="1"
											variant="soft"
											onClick={() => openSearch(platform)}
										>
											搜索候选
										</Button>
										{confirmed && (
											<Button
												size="1"
												variant="soft"
												color="red"
												disabled={busyKey === `remove:${platform}`}
												onClick={() => void remove(platform)}
											>
												移除
											</Button>
										)}
									</Flex>
								</Flex>
							);
						})}
					</Flex>
					{activePlatform && (
						<Flex direction="column" gap="2" className={styles.searchBox}>
							<Text size="2" weight="bold">
								在 {platformDisplayNames[activePlatform]} 搜索
							</Text>
							<Flex gap="2">
								<TextField.Root
									className={styles.grow}
									value={query}
									placeholder="歌曲名 歌手"
									onChange={(event) => setQuery(event.target.value)}
									onKeyDown={(event) => {
										if (event.key === "Enter")
											void search(activePlatform, query);
									}}
								/>
								<Button
									variant="soft"
									disabled={searching}
									onClick={() => void search(activePlatform, query)}
								>
									{searching && <Spinner size="1" />}
									搜索
								</Button>
							</Flex>
							<Flex direction="column" className={styles.candidateList}>
								{searching && (
									<Text size="1" color="gray" className={styles.candidateEmpty}>
										正在搜索…
									</Text>
								)}
								{!searching && candidates?.length === 0 && (
									<Text size="1" color="gray" className={styles.candidateEmpty}>
										没有找到候选，请调整关键词。
									</Text>
								)}
								{!searching &&
									candidates?.map((candidate) => (
										<Flex
											key={candidate.track_id}
											className={styles.candidateRow}
											justify="between"
											align="center"
											gap="3"
										>
											<Flex
												direction="column"
												gap="1"
												className={styles.grow}
											>
												<Flex align="center" gap="2" wrap="wrap">
													<Text size="2" weight="medium">
														{candidate.title}
													</Text>
													<Badge
														color={
															candidate.score >= 85
																? "green"
																: candidate.score >= 60
																	? "amber"
																	: "gray"
														}
													>
														{candidate.score} 分
													</Badge>
													{candidate.selected && (
														<Badge color="blue">当前</Badge>
													)}
												</Flex>
												<Text size="1" color="gray">
													{[
														candidate.artists?.join(" / "),
														candidate.album,
														formatDuration(candidate.duration_ms),
														candidate.isrc,
													]
														.filter(Boolean)
														.join(" · ")}
												</Text>
												{(candidate.reasons?.length ?? 0) > 0 && (
													<Flex gap="1" wrap="wrap">
														{candidate.reasons?.map((reason) => (
															<Badge
																key={reason}
																variant="soft"
																color="gray"
																size="1"
															>
																{reasonLabels[reason] ?? reason}
															</Badge>
														))}
													</Flex>
												)}
											</Flex>
											<Button
												size="1"
												disabled={
													busyKey ===
													`${candidate.platform}:${candidate.track_id}`
												}
												onClick={() =>
													void confirm(candidate.platform, candidate.track_id)
												}
											>
												{busyKey ===
													`${candidate.platform}:${candidate.track_id}` && (
													<Spinner size="1" />
												)}
												导入
											</Button>
										</Flex>
									))}
							</Flex>
						</Flex>
					)}
					<Flex justify="end">
						<Dialog.Close>
							<Button variant="soft" color="gray">
								关闭
							</Button>
						</Dialog.Close>
					</Flex>
				</Flex>
			</Dialog.Content>
		</Dialog.Root>
	);
}
