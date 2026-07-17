import { githubFetch } from "$/modules/github/api";

export type CreateIssueOptions = {
	token: string;
	repoOwner: string;
	repoName: string;
	title: string;
	body: string;
	labels?: string[];
	assignees?: string[];
};

export type CreateIssueResult = {
	ok: boolean;
	status?: number;
	message?: string;
	issueNumber?: number;
	issueUrl?: string;
};

const parseErrorDetail = async (response: Response) => {
	const errorText = await response.text();
	if (!errorText) return "";
	try {
		const parsed = JSON.parse(errorText) as {
			message?: string;
			errors?: unknown;
		};
		const message = parsed.message ?? errorText;
		return parsed.errors !== undefined
			? `${message}: ${JSON.stringify(parsed.errors)}`
			: message;
	} catch {
		return errorText;
	}
};

export const createGithubIssue = async (
	options: CreateIssueOptions,
): Promise<CreateIssueResult> => {
	const headers = {
		Accept: "application/vnd.github+json",
		Authorization: `Bearer ${options.token}`,
		"X-GitHub-Api-Version": "2022-11-28",
		"Content-Type": "application/json",
	};
	const response = await githubFetch(
		`/repos/${options.repoOwner}/${options.repoName}/issues`,
		{
			init: {
				method: "POST",
				headers,
				body: JSON.stringify({
					title: options.title,
					body: options.body,
					...(options.labels ? { labels: options.labels } : {}),
					...(options.assignees ? { assignees: options.assignees } : {}),
				}),
			},
		},
	);
	if (!response.ok) {
		const message = await parseErrorDetail(response);
		return { ok: false, status: response.status, message };
	}
	const data = (await response.json()) as {
		number?: number;
		html_url?: string;
	};
	return {
		ok: true,
		issueNumber: data.number,
		issueUrl: data.html_url,
	};
};
