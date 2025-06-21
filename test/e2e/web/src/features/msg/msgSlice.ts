import { createAppSlice } from "../../app/createAppSlice";
import { postMsg, postMsgToSomeone } from "./msgApi";

interface HelloState {
	readonly success: string;
	readonly error: string;
}

const initialHelloState = (): HelloState => ({
	success: "",
	error: ""
});

export interface MessaggeSliceState {
	readonly sayHello: HelloState;
	readonly sayHelloToSomeone: HelloState;
}

const initialState: MessaggeSliceState = {
	sayHello: initialHelloState(),
	sayHelloToSomeone: initialHelloState()
};

export const msgTestsSlice = createAppSlice({
	name: "msgTests",
	initialState,
	reducers: create => ({
		sayHello: create.asyncThunk(
			async () => {
				const response = await postMsg();
				return response.result;
			},
			{
				pending: () => {
					console.log("sayHello pending...");
				},
				fulfilled: (state, action) => {
					state.sayHello.success = action.payload;
					state.sayHello.error = "";
				},
				rejected: (state, action) => {
					state.sayHello.error = action.error.message as string;
					state.sayHello.success = "";
				}
			}
		),
		sayHelloToSomeone: create.asyncThunk(
			async (toWhom: string[]) => {
				const response = await postMsgToSomeone(toWhom);
				return response.result;
			},
			{
				pending: () => {
					console.log("sayHelloToSomeone pending...");
				},
				fulfilled: (state, action) => {
					state.sayHelloToSomeone.success = action.payload;
					state.sayHelloToSomeone.error = "";
				},
				rejected: (state, action) => {
					state.sayHelloToSomeone.error = action.error.message as string;
					state.sayHelloToSomeone.success = "";
				}
			}
		)
	}),
	selectors: {
		selectHelloResult: msgTests => msgTests.sayHello,
		selectHelloToSomeoneResult: msgTests => msgTests.sayHelloToSomeone
	}
});

export const { sayHello, sayHelloToSomeone } =
	msgTestsSlice.actions;

export const { selectHelloResult, selectHelloToSomeoneResult } =
	msgTestsSlice.selectors;
