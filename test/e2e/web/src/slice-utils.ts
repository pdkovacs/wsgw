export enum AsyncValueStatus {
	initial = "initial",
	pending = "pending",
	resolved = "resolved",
	failedToResolve = "failedToResolve"
}

export interface AsyncValue<T> {
	readonly status: AsyncValueStatus;
	readonly value: T | null;
}
