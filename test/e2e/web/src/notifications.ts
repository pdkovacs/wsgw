
const initialReconnectInterval = () => 3 + 2 * Math.random();

export const dial = (host: string, port: number, retryCount: number, messageReceived: (message: string, error: Error | null) => void) => {

	const reconnectInterval = retryCount === 0 ? initialReconnectInterval() : Math.exp(retryCount);

	const conn = new WebSocket(`ws://${host}:${port}/connect`);

	conn.addEventListener("close", ev => {
		console.log(`WebSocket Disconnected code: ${ev.code}, reason: ${ev.reason}`, true);
		if (ev.code !== 1001) {
			console.log("Reconnecting in ", reconnectInterval);
			setTimeout(() => dial(host, port, retryCount + 1, messageReceived), reconnectInterval * 1000);
		}
	});

	conn.addEventListener("open", () => {
		console.info("websocket connected");
		retryCount = 0;
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
