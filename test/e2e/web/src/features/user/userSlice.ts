import { createAppSlice } from "../../app/createAppSlice";
import type { UserInfo } from "../../dto/UserInfo";
import { AsyncValue, AsyncValueStatus } from "../../slice-utils";
import { fetchUserInfo, fetchUserList } from "./userApi";

export interface UserSliceState {
	readonly userInfo: AsyncValue<UserInfo>;
	readonly users: AsyncValue<string[]>;
}

const initialState: UserSliceState = {
	userInfo: {
		status: AsyncValueStatus.initial,
		value: null
	},
	users: {
		status: AsyncValueStatus.initial,
		value: []
	}
};

export const userSlice = createAppSlice({
	name: "user",
	initialState,
	reducers: create => ({
		getUserInfo: create.asyncThunk(
			async () => {
				const response = await fetchUserInfo();
				return response.userInfo;
			},
			{
				pending: state => {
					state.userInfo.status = AsyncValueStatus.pending;
				},
				fulfilled: (state, action) => {
					state.userInfo.status = AsyncValueStatus.resolved;
					state.userInfo.value = action.payload;
				},
				rejected: state => {
					state.userInfo.status = AsyncValueStatus.initial;
				}
			}
		),
		getUserList: create.asyncThunk(
			async () => {
				const response = await fetchUserList();
				return response;
			},
			{
				pending: state => {
					state.users.status = AsyncValueStatus.pending;
				},
				fulfilled: (state, action) => {
					state.users.status = AsyncValueStatus.resolved;
					state.users.value = action.payload;
				},
				rejected: state => {
					state.users.status = AsyncValueStatus.initial;
				}
			}
		)
	}),
	selectors: {
		selectUserInfo: user => user.userInfo
	}
});

export const { getUserInfo, getUserList } = userSlice.actions;

export const { selectUserInfo } = userSlice.selectors;
