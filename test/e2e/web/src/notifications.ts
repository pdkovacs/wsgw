export const dial = (host: string, port: number, messageReceived: (message: string, error: Error | null) => void) => {

	const conn = new WebSocket(`ws://${host}:${port}/connect`);

	conn.addEventListener("close", ev => {
		console.log(`WebSocket Disconnected code: ${ev.code}, reason: ${ev.reason}`, true);
		if (ev.code !== 1001) {
			console.log("Reconnecting in 5s", true);
			setTimeout(() => dial(host, port, messageReceived), 5000);
		}
	});

	conn.addEventListener("open", () => {
		console.info("websocket connected");
	});

	// This is where we handle messages received.
	conn.addEventListener("message", ev => {
		if (typeof ev.data !== "string") {
			console.error("unexpected message type", typeof ev.data);
			return;
		}
		console.log(ev.data);
		messageReceived(ev.data, null);
	});
};
