import type { NeteaseProfile } from "$/modules/settings/states";
import { requestNetease } from "./index";
import type { NeteaseResponse } from "./index";

const autoLoginFailureKey = "neteaseAutoLoginFailures";
const maxAutoLoginFailures = 3;

const readAutoLoginFailures = () => {
	if (typeof localStorage === "undefined") {
		return 0;
	}
	try {
		const raw = localStorage.getItem(autoLoginFailureKey);
		if (!raw) return 0;
		const parsed = Number.parseInt(raw, 10);
		return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
	} catch {
		return 0;
	}
};

const writeAutoLoginFailures = (value: number) => {
	if (typeof localStorage === "undefined") {
		return;
	}
	try {
		localStorage.setItem(autoLoginFailureKey, String(value));
	} catch {
		return;
	}
};

export const NeteaseAutoLoginGuard = {
	maxFailures: maxAutoLoginFailures,
	getFailures: () => readAutoLoginFailures(),
	shouldAttempt: () => readAutoLoginFailures() < maxAutoLoginFailures,
	recordFailure: () => {
		const nextValue = readAutoLoginFailures() + 1;
		writeAutoLoginFailures(nextValue);
		return nextValue;
	},
	reset: () => writeAutoLoginFailures(0),
};

export const NeteaseAuthClient = {
	sendCaptcha: async (phone: string, ctcode = "86") => {
		return requestNetease<NeteaseResponse<boolean>>("/captcha/sent", {
			params: { phone, ctcode },
		});
	},
	loginByPhone: async (phone: string, captcha: string, ctcode = "86") => {
		const res = await requestNetease<
			NeteaseResponse<Record<string, unknown>> & {
				profile: NeteaseProfile;
				cookie: string;
			}
		>("/login/cellphone", {
			params: { phone, captcha, ctcode },
		});

		return {
			cookie: res.cookie ?? "",
			profile: res.profile,
		};
	},
	checkCookieStatus: async (cookieString: string) => {
		const res = await requestNetease<{
			data: {
				profile: NeteaseProfile | null;
				account?: { vipType: number; id: number };
			};
		}>("/login/status", {
			cookie: cookieString,
			method: "POST",
		});

		const profile = res.data?.profile;
		const account = res.data?.account;

		if (profile) {
			if (account && typeof account.vipType === "number") {
				return {
					...profile,
					vipType: account.vipType,
				};
			}
			return profile;
		}
		throw new Error("Cookie 已失效或未登录");
	},
};
