import { createAppSlice } from "../../app/createAppSlice";
import { AsyncValue, AsyncValueStatus } from "../../slice-utils";
import { Configuration, getConfig as getConfigApi } from "./configApi";

interface ConfigurationSliceState {
	readonly configuration: AsyncValue<Configuration>;
}

const initialState: ConfigurationSliceState = {
	configuration: {
		status: AsyncValueStatus.initial,
		value: { wsgwHost: "", wsgwPort: -1 } as Configuration
	}
};

export const configurationSlice = createAppSlice({
	name: "configuration",
	initialState,
	reducers: create => ({
		getConfig: create.asyncThunk(
			async () => {
				const response = await getConfigApi();
				return response;
			},
			{
				pending: state => {
					state.configuration.status = AsyncValueStatus.pending;
				},
				fulfilled: (state, action) => {
					state.configuration.value = action.payload;
					state.configuration.status = AsyncValueStatus.resolved;
				},
				rejected: state => {
					console.error("getConfig rejected");
					state.configuration.status = AsyncValueStatus.failedToResolve;
				}
			}
		)
	}),
	selectors: {
		selectConfiguration: slice => slice.configuration
	}
});

export const { getConfig } = configurationSlice.actions;
export const { selectConfiguration } = configurationSlice.selectors;
