import { Button, Dialog, Flex, Heading, Spinner, Text } from "@radix-ui/themes";
import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";
import { useFileOpener } from "$/hooks/useFileOpener";
import exportTTMLText from "$/modules/project/logic/ttml-writer";
import { lyricLinesAtom } from "$/states/main";
import { pushNotificationAtom } from "$/states/notifications";
import styles from "./AutoAlignmentPanel.module.css";
import { musicWebProjectID } from "./metadata";

type Capability = {
	enabled: boolean;
	ready: boolean;
	reason?: string;
	engine: string;
	max_concurrent: number;
};

type AlignmentJob = {
	job_id: string;
	status: "queued" | "running" | "ready" | "failed";
	stage: string;
	progress: number;
	message?: string;
	error?: string;
	tokens?: number;
	low_confidence_tokens?: number;
	result_url?: string;
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

const stageName = (stage?: string) => {
	switch (stage) {
		case "queued":
			return "等待执行";
		case "preparing":
			return "准备歌词";
		case "separating":
			return "分离人声";
		case "normalizing":
			return "标准化音频";
		case "aligning":
			return "逐字对齐";
		case "writing":
			return "生成 TTML";
		case "ready":
			return "已完成";
		case "failed":
			return "失败";
		default:
			return "尚未开始";
	}
};

const wait = (milliseconds: number) =>
	new Promise((resolve) => window.setTimeout(resolve, milliseconds));

export function AutoAlignmentPanel() {
	const projectID = musicWebProjectID();
	const lyrics = useAtomValue(lyricLinesAtom);
	const notify = useSetAtom(pushNotificationAtom);
	const { openFile } = useFileOpener();
	const [open, setOpen] = useState(false);
	const [capability, setCapability] = useState<Capability>();
	const [job, setJob] = useState<AlignmentJob>();
	const [starting, setStarting] = useState(false);
	const cancelled = useRef(false);

	useEffect(() => {
		if (!projectID) return;
		void api<Capability>("/api/v1/studio/alignment")
			.then(setCapability)
			.catch((error) =>
				setCapability({
					enabled: false,
					ready: false,
					reason: error.message,
					engine: "自动打轴服务",
					max_concurrent: 0,
				}),
			);
	}, [projectID]);

	useEffect(
		() => () => {
			cancelled.current = true;
		},
		[],
	);

	const importResult = useCallback(
		async (finished: AlignmentJob) => {
			if (!finished.result_url) throw new Error("任务没有返回 TTML 地址");
			const response = await fetch(finished.result_url);
			if (!response.ok)
				throw new Error(`读取 TTML 失败：HTTP ${response.status}`);
			const content = await response.text();
			await openFile(
				new File([content], `${projectID}.auto-draft.ttml`, {
					type: "application/ttml+xml",
				}),
				"ttml",
			);
			notify({
				title: `自动打轴已导入：${finished.tokens || 0} 个字词，${finished.low_confidence_tokens || 0} 个低置信度位置待复核`,
				level: "success",
				source: "AI 自动打轴",
			});
		},
		[notify, openFile, projectID],
	);

	const poll = useCallback(
		async (initial: AlignmentJob) => {
			let current = initial;
			while (!cancelled.current) {
				setJob(current);
				if (current.status === "ready") {
					await importResult(current);
					return;
				}
				if (current.status === "failed") {
					throw new Error(current.error || current.message || "自动打轴失败");
				}
				await wait(1500);
				current = await api<AlignmentJob>(
					`/api/v1/studio/projects/${encodeURIComponent(projectID)}/alignments/${encodeURIComponent(current.job_id)}`,
				);
			}
		},
		[importResult, projectID],
	);

	const start = useCallback(async () => {
		if (
			!projectID ||
			starting ||
			(job && !["ready", "failed"].includes(job.status))
		)
			return;
		setStarting(true);
		cancelled.current = false;
		try {
			const content = exportTTMLText(lyrics);
			const created = await api<AlignmentJob>(
				`/api/v1/studio/projects/${encodeURIComponent(projectID)}/alignments`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ content }),
				},
			);
			setJob(created);
			await poll(created);
		} catch (error) {
			const message = error instanceof Error ? error.message : String(error);
			notify({
				title: `自动打轴失败：${message}`,
				level: "error",
				source: "AI 自动打轴",
			});
			setJob((current) =>
				current
					? { ...current, status: "failed", stage: "failed", error: message }
					: current,
			);
		} finally {
			setStarting(false);
		}
	}, [job, lyrics, notify, poll, projectID, starting]);

	if (!projectID) return null;
	const running =
		starting || (job && ["queued", "running"].includes(job.status));
	const progress = Math.max(0, Math.min(100, job?.progress || 0));

	return (
		<Dialog.Root open={open} onOpenChange={setOpen}>
			<Dialog.Trigger>
				<Button className={styles.trigger} size="2" variant="soft">
					AI 自动打轴
				</Button>
			</Dialog.Trigger>
			<Dialog.Content className={styles.content}>
				<Dialog.Title>AI 自动逐字打轴</Dialog.Title>
				<Dialog.Description>
					使用当前歌词文本和项目音频生成逐字 TTML 草稿。
				</Dialog.Description>
				<Flex direction="column" gap="4" mt="4">
					<Flex direction="column" gap="1" className={styles.engine}>
						<Text size="2" weight="bold">
							{capability?.engine || "正在检测服务…"}
						</Text>
						<Text size="1" color={capability?.ready ? "green" : "gray"}>
							{capability?.ready
								? "服务已就绪"
								: capability?.reason || "正在读取能力信息"}
						</Text>
					</Flex>
					<div className={styles.warning}>
						<Text size="2">
							生成结果会替换编辑器中的当前时间轴；主唱与和声重叠处仍需人工复核。建议先等待自动保存完成。
						</Text>
					</div>
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
								? "自动打轴中"
								: job?.status === "ready"
									? "重新生成"
									: "开始自动打轴"}
						</Button>
					</Flex>
				</Flex>
			</Dialog.Content>
		</Dialog.Root>
	);
}
