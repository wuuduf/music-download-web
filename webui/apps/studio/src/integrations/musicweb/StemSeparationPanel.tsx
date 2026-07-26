import {
	Button,
	Checkbox,
	Dialog,
	Flex,
	Heading,
	Spinner,
	Text,
} from "@radix-ui/themes";
import { useSetAtom } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";
import { pushNotificationAtom } from "$/states/notifications";
import { api, wait } from "./api";
import { musicWebProjectID } from "./metadata";
import styles from "./StemSeparationPanel.module.css";

type Capability = {
	enabled: boolean;
	ready: boolean;
	reason?: string;
	engine: string;
	base_model: string;
	max_concurrent: number;
	refinement_enabled: boolean;
	refinement_ready: boolean;
	refinement_reason?: string;
	configured_refiners?: string[];
};

type StemTrack = {
	id: string;
	label: string;
	stem: string;
	kind: "base" | "refined" | "derived";
	source: string;
	url: string;
	preferred: boolean;
	parent?: string;
	duration_ms?: number;
	size_bytes?: number;
};

type StemJob = {
	job_id: string;
	status: "queued" | "running" | "ready" | "failed";
	stage: string;
	progress: number;
	message?: string;
	error?: string;
	refinement_requested: boolean;
	warnings?: string[];
	tracks?: StemTrack[];
};

const stageName = (stage?: string) => {
	const stages: Record<string, string> = {
		queued: "等待执行",
		preparing: "准备音频",
		base_separation: "Demucs 六轨分离",
		refine_vocals: "Mel-Band RoFormer 人声精修",
		separate_backing: "分离主唱与和声",
		refine_piano: "钢琴精修",
		refine_guitar: "吉他精修",
		writing_manifest: "整理分轨文件",
		ready: "分轨完成",
		failed: "分轨失败",
	};
	return stages[stage || ""] || "尚未开始";
};

const formatSize = (bytes?: number) => {
	if (!bytes) return "";
	return bytes >= 1024 * 1024
		? `${(bytes / 1024 / 1024).toFixed(1)} MB`
		: `${Math.ceil(bytes / 1024)} KB`;
};

export function StemSeparationPanel() {
	const projectID = musicWebProjectID();
	const notify = useSetAtom(pushNotificationAtom);
	const [open, setOpen] = useState(false);
	const [capability, setCapability] = useState<Capability>();
	const [job, setJob] = useState<StemJob>();
	const [refine, setRefine] = useState(false);
	const [starting, setStarting] = useState(false);
	const cancelled = useRef(false);

	useEffect(() => {
		if (!projectID) return;
		void api<Capability>("/api/v1/studio/stems")
			.then((value) => {
				setCapability(value);
				setRefine(value.refinement_ready);
			})
			.catch((error) =>
				setCapability({
					enabled: false,
					ready: false,
					reason: error.message,
					engine: "多阶段分轨服务",
					base_model: "htdemucs_6s",
					max_concurrent: 0,
					refinement_enabled: false,
					refinement_ready: false,
				}),
			);
	}, [projectID]);

	useEffect(
		() => () => {
			cancelled.current = true;
		},
		[],
	);

	const poll = useCallback(
		async (initial: StemJob) => {
			let current = initial;
			while (!cancelled.current) {
				setJob(current);
				if (current.status === "ready") {
					notify({
						title: `分轨完成：生成 ${current.tracks?.length || 0} 条轨道`,
						level: "success",
						source: "AI 多阶段分轨",
					});
					return;
				}
				if (current.status === "failed")
					throw new Error(current.error || current.message || "分轨失败");
				await wait(1800);
				current = await api<StemJob>(
					`/api/v1/studio/projects/${encodeURIComponent(projectID)}/stems/${encodeURIComponent(current.job_id)}`,
				);
			}
		},
		[notify, projectID],
	);

	const start = useCallback(async () => {
		if (!projectID || starting) return;
		setStarting(true);
		cancelled.current = false;
		try {
			const created = await api<StemJob>(
				`/api/v1/studio/projects/${encodeURIComponent(projectID)}/stems`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ refine }),
				},
			);
			setJob(created);
			await poll(created);
		} catch (error) {
			const message = error instanceof Error ? error.message : String(error);
			notify({
				title: `分轨失败：${message}`,
				level: "error",
				source: "AI 多阶段分轨",
			});
			setJob((current) =>
				current
					? { ...current, status: "failed", stage: "failed", error: message }
					: current,
			);
		} finally {
			setStarting(false);
		}
	}, [notify, poll, projectID, refine, starting]);

	if (!projectID) return null;
	const running =
		starting || Boolean(job && ["queued", "running"].includes(job.status));
	const progress = Math.max(0, Math.min(100, job?.progress || 0));
	const tracks = job?.tracks || [];

	return (
		<Dialog.Root open={open} onOpenChange={setOpen}>
			<Dialog.Trigger>
				<Button className={styles.trigger} size="2" variant="soft">
					AI 六轨分离
				</Button>
			</Dialog.Trigger>
			<Dialog.Content className={styles.content}>
				<Dialog.Title>多阶段乐器与人声分轨</Dialog.Title>
				<Dialog.Description>
					先用 htdemucs_6s 生成六条基础轨，再按配置精修人声、和声、钢琴和吉他。
				</Dialog.Description>
				<Flex direction="column" gap="4" mt="4">
					<div className={styles.engine}>
						<Text size="2" weight="bold">
							{capability?.engine || "正在检测服务…"}
						</Text>
						<Text size="1" color={capability?.ready ? "green" : "gray"}>
							{capability?.ready
								? `基础模型 ${capability.base_model} 已就绪`
								: capability?.reason || "正在读取能力信息"}
						</Text>
					</div>
					<label className={styles.refineOption}>
						<Checkbox
							checked={refine}
							disabled={!capability?.refinement_ready || Boolean(running)}
							onCheckedChange={(checked) => setRefine(checked === true)}
						/>
						<Flex direction="column" gap="1">
							<Text size="2" weight="bold">
								启用第二阶段精修
							</Text>
							<Text size="1" color="gray">
								{capability?.refinement_ready
									? "精修失败时会自动保留 Demucs 基础轨，不影响任务完成。"
									: capability?.refinement_reason || "精修运行时尚未配置"}
							</Text>
						</Flex>
					</label>
					{job && (
						<Flex direction="column" gap="2">
							<Flex justify="between" align="center">
								<Heading size="3">{stageName(job.stage)}</Heading>
								<Text size="2">{progress}%</Text>
							</Flex>
							<div className={styles.progressTrack}>
								<div
									className={styles.progressValue}
									style={{ width: `${progress}%` }}
								/>
							</div>
							<Text size="2" color={job.status === "failed" ? "red" : "gray"}>
								{job.error || job.message || "处理中…"}
							</Text>
						</Flex>
					)}
					{Boolean(job?.warnings?.length) && (
						<div className={styles.warning}>
							{job?.warnings?.map((warning) => (
								<Text key={warning} size="1">
									{warning}
								</Text>
							))}
						</div>
					)}
					{tracks.length > 0 && (
						<div className={styles.trackGrid}>
							{tracks.map((track) => (
								<div
									className={`${styles.trackCard} ${track.preferred ? styles.preferred : ""}`}
									key={track.id}
								>
									<Flex justify="between" align="start" gap="2">
										<Flex direction="column" gap="1">
											<Text size="2" weight="bold">
												{track.label}
											</Text>
											<Text size="1" color="gray">
												{track.kind} · {track.source}{" "}
												{formatSize(track.size_bytes)}
											</Text>
										</Flex>
										{track.preferred && (
											<span className={styles.badge}>推荐</span>
										)}
									</Flex>
									<audio controls preload="none" src={track.url} />
									<a href={track.url} download={`${track.id}.wav`}>
										下载 WAV
									</a>
								</div>
							))}
						</div>
					)}
					<Flex justify="end" gap="3">
						<Dialog.Close>
							<Button variant="soft" color="gray">
								关闭
							</Button>
						</Dialog.Close>
						<Button
							disabled={!capability?.ready || Boolean(running)}
							onClick={() => void start()}
						>
							{running && <Spinner />}
							{running
								? "分轨中"
								: job?.status === "ready"
									? "重新分轨"
									: "开始分轨"}
						</Button>
					</Flex>
				</Flex>
			</Dialog.Content>
		</Dialog.Root>
	);
}
