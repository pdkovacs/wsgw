import axios from "axios";

export interface Configuration {
	readonly wsgwHost: string;
	readonly wsgwPort: number;
}

let configuration: Configuration;

export const getConfig = async (): Promise<Configuration> => {
	const response = await axios({
		method: "GET",
		url: "/config"
	});
	configuration = response.data;
	return configuration;
};
