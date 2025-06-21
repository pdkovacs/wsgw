import React from "react";
import { Button, TextField } from "@mui/material";

import "./Msg.css";
import { useAppDispatch, useAppSelector } from "../../app/hooks";
import { sayHello, sayHelloToSomeone, selectHelloResult, selectHelloToSomeoneResult } from "./msgSlice";
import { useState } from "react";

export const Msg = () => {
	return <div className="msg-tests-container">
		<SayHello />
		<SayHelloToSomeone />
	</div>;
};

const SayHello = () => {

	const { success, error } = useAppSelector(selectHelloResult);

	const dispatch = useAppDispatch();

	return (
		<div className="say-hello-container say-hello">
			<Button onClick={() => {
				dispatch(sayHello());
			}}>Say hello to everybody</Button>
			<div></div>
			<div className="say-hello-result">{
				success && <div>{success}</div> ||
				error && <div className="error">{error}</div>
			}</div>
		</div>
	);
};

const SayHelloToSomeone = () => {

	const { success, error } = useAppSelector(selectHelloToSomeoneResult);

	const [whom, setWhom] = useState<string>("");

	const dispatch = useAppDispatch();

	return (
		<div className="say-hello-container say-hello-to-someone">
			<Button onClick={() => {
				dispatch(sayHelloToSomeone(whom.split(" ")));
			}}>Say hello to</Button>
			<TextField
				placeholder="whom?"
				value={whom}
				onChange={event => {
					setWhom(event.target.value);
				}}
			/>
			<div className="say-hello-result">{
				success && <div>{success}</div> ||
				error && <div className="error">{error}</div>
			}</div>
		</div>
	);
};
